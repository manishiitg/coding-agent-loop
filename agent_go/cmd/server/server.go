package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" // Register pprof handlers
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent-builder-go/agent_go/internal/events"
	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
	"mcp-agent-builder-go/agent_go/pkg/chathistory"
	"mcp-agent-builder-go/agent_go/pkg/costledger"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	orchEvents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	orchtypes "mcp-agent-builder-go/agent_go/pkg/orchestrator/types"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"

	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/pkg/browser"
	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/logger"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/subagents"

	"github.com/joho/godotenv"

	eventbridge "mcp-agent-builder-go/agent_go/cmd/server/event_bridge"
	"mcp-agent-builder-go/agent_go/cmd/server/guidance"
	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	browserinstructions "mcp-agent-builder-go/agent_go/pkg/instructions"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
	"strconv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/agent/prompt"
)

// ServerCmd represents the server command
var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the streaming API server",
	Long: `Start the streaming API server that provides HTTP endpoints and Server-Sent Events (SSE) support 
for real-time agent streaming. This server enables frontend integration with the MCP agent.

The server provides:
- REST API endpoints for agent queries
- Server-Sent Events (SSE) for real-time streaming
- Polling API for event retrieval
- Multi-provider LLM support (Bedrock, OpenAI, Anthropic)
- Full observability and tracing

Examples:
  mcp-agent server                           # Start server with default settings
  mcp-agent server --port 8000              # Start on custom port
  mcp-agent server --provider openai        # Use OpenAI provider
  mcp-agent server --cors-origins "*"       # Enable CORS for all origins`,
	Run: runServer,
}

// Server configuration
type ServerConfig struct {
	Port          int      `json:"port"`
	Host          string   `json:"host"`
	CORSOrigins   []string `json:"cors_origins"`
	Provider      string   `json:"provider"`
	ModelID       string   `json:"model_id"`
	Temperature   float64  `json:"temperature"`
	MaxTurns      int      `json:"max_turns"`
	MCPConfigPath string   `json:"mcp_config_path"`
}

// ActiveSessionInfo represents an active session for page refresh recovery
type ActiveSessionInfo struct {
	SessionID                   string     `json:"session_id"`
	AgentMode                   string     `json:"agent_mode"`
	Status                      string     `json:"status"` // "running", "paused", "completed"
	LastActivity                time.Time  `json:"last_activity"`
	CreatedAt                   time.Time  `json:"created_at"`
	Query                       string     `json:"query,omitempty"`
	Title                       string     `json:"title,omitempty"`
	WorkflowName                string     `json:"workflow_name,omitempty"`
	WorkflowLabel               string     `json:"workflow_label,omitempty"`
	WorkspacePath               string     `json:"workspace_path,omitempty"`
	PresetName                  string     `json:"preset_name,omitempty"`
	PresetQueryID               string     `json:"preset_query_id,omitempty"`
	BotPlatform                 string     `json:"bot_platform,omitempty"`
	TriggeredBy                 string     `json:"triggered_by,omitempty"`
	LLMGuidance                 string     `json:"llm_guidance,omitempty"`  // LLM guidance message for this session
	MemoryFolder                string     `json:"memory_folder,omitempty"` // Per-user memory folder (default: _users/<userID>/memories)
	ChatsFolder                 string     `json:"chats_folder,omitempty"`  // Per-user Chats folder (default: _users/<userID>/Chats)
	UserID                      string     `json:"-"`                       // User ID for session isolation (not exposed in JSON)
	IsSyntheticTurn             bool       `json:"is_synthetic_turn"`       // True when running an auto-notification turn (not user-initiated)
	HasRunningBackgroundAgents  bool       `json:"has_running_background_agents,omitempty"`
	RunningBackgroundAgentCount int        `json:"running_background_agent_count,omitempty"`
	CurrentExecutionName        string     `json:"current_execution_name,omitempty"`
	NeedsUserInput              bool       `json:"needs_user_input,omitempty"`
	WaitingEventType            string     `json:"waiting_event_type,omitempty"`
	WaitingMessage              string     `json:"waiting_message,omitempty"`
	WaitingSince                *time.Time `json:"waiting_since,omitempty"`
}

// StreamingAPI represents the streaming API server
type StreamingAPI struct {
	config ServerConfig

	// Note: Removed session management - fresh agents created per request

	// Agent cancel functions for proper context cancellation: sessionID -> context.CancelFunc
	agentCancelFuncs map[string]context.CancelFunc
	agentCancelMux   sync.RWMutex

	// Workflow orchestrator sessions: sessionID -> orchestrator.Orchestrator

	// Workflow orchestrator contexts for cancellation: queryID -> context.CancelFunc
	// Using queryID (not sessionID) ensures each execution is independent
	workflowOrchestratorContexts   map[string]context.CancelFunc
	workflowOrchestratorContextMux sync.RWMutex

	// Active workflow executions registry (in-memory, source of truth for "currently running")
	activeWorkflowExecutions    map[string]*ActiveWorkflowExecution // queryID -> execution info
	activeWorkflowExecutionsMux sync.RWMutex

	// Unified execution tracker for top-level workflow runs and workflow-builder background work.
	trackedWorkflowExecutions    map[string]*TrackedWorkflowExecution
	trackedWorkflowExecutionsMux sync.RWMutex

	// Mapping of sessionID -> []queryID to track which executions belong to which session
	// Used by handleStopSession to cancel all executions for a session
	sessionQueryIDs   map[string][]string
	sessionQueryIDMux sync.RWMutex

	// Workflow objectives: sessionID -> objective
	workflowObjectives   map[string]string
	workflowObjectiveMux sync.RWMutex

	// Workflow step IDs: presetQueryID -> stepID (temporary storage for step-specific phase execution)
	workflowStepIDs   map[string]string
	workflowStepIDMux sync.RWMutex

	// Conversation history storage: sessionID -> conversation history
	conversationHistory map[string][]llmtypes.MessageContent
	conversationMux     sync.RWMutex

	// Operator-state store: bot connector configs + per-user encrypted secrets.
	chatStore chathistory.Store

	// Global append-only cost ledger — one line per token_usage event.
	costLedger *costledger.Ledger

	// Polling system components
	eventStore *events.EventStore

	// Workflow orchestrator configuration
	provider      string
	model         string
	mcpConfigPath string
	temperature   float64
	workspaceRoot string

	// Active session tracking for page refresh recovery
	activeSessions    map[string]*ActiveSessionInfo
	activeSessionsMux sync.RWMutex

	//nolint:unused // kept for the pending session-reactivation path during the tracker refactor.
	// Session reactivation lock: prevents race conditions when calculating baseIndex
	// and initializing the event store for reactivated sessions
	sessionReactivationMux sync.Mutex

	// stoppedSessions tracks sessions that the user explicitly stopped.
	//
	// BUG FIX (2026-04-04): Race condition between stop and in-flight queries.
	// Timeline of the bug:
	//   1. User clicks stop → handleStopSession closes the WorkshopChatSession
	//      (cancels its context.Background()-derived ctx) and deletes it from
	//      workshopChatSessions map.
	//   2. An in-flight query goroutine (started before or concurrently with stop)
	//      reaches the workshop-creation code and calls NewWorkshopChatSession(),
	//      creating a FRESH session with a new context.Background() — completely
	//      detached from any cancellation.
	//   3. This new workshop spawns step execution goroutines (group sessions,
	//      isolated Codex CLI processes) that are never canceled because no
	//      subsequent stop targets the new workshop.
	//
	// Fix: handleStopSession adds the sessionID here. The query handler checks
	// this set before creating/reusing workshop sessions and bails early.
	// The flag is cleared when the session is explicitly reactivated by a
	// new user message (not by a racing goroutine).
	stoppedSessions   map[string]bool
	stoppedSessionsMu sync.RWMutex

	// Orchestrator objects in memory for guidance injection
	workflowOrchestrators    map[string]orchestrator.Orchestrator
	workflowOrchestratorsMux sync.RWMutex

	// Background agent registry for async delegation in multi-agent mode
	bgAgentRegistry *BackgroundAgentRegistry

	// Session busy tracking — prevents synthetic turns from overlapping with user turns
	sessionBusy   map[string]bool
	sessionBusyMu sync.RWMutex

	// Pending completions queue — background agent IDs that finished while session was busy
	pendingCompletions map[string][]string
	pendingMu          sync.RWMutex

	// Last query request per session — used to construct synthetic turns
	lastQueryRequests       map[string]QueryRequest
	lastQueryMu             sync.RWMutex
	sessionWorkspaceFolders map[string]string // sessionID → resolved workflowPhaseFolder (for builder log persistence in synthetic turns)
	sessionWorkspaceMu      sync.RWMutex

	// Stored agent instances for synthetic turns (plan mode only)
	// Reused directly via StreamWithEvents() instead of re-creating agents per synthetic turn
	sessionAgents    map[string]*agent.LLMAgentWrapper
	sessionAgentsMux sync.RWMutex

	// Running agent references for steer message injection (chat mode)
	// Written when agent starts, deleted when streaming completes
	runningAgents    map[string]*mcpagent.Agent
	runningAgentsMux sync.RWMutex

	// Claude Code CLI session IDs for --resume (our sessionID -> CLI session_id)
	claudeCodeSessionIDs map[string]string

	// Gemini CLI session IDs for --resume (our sessionID -> CLI session_id)
	geminiSessionIDs map[string]string

	// Gemini CLI project directory IDs for per-invocation isolation (our sessionID -> dir ID)
	geminiProjectDirIDs map[string]string

	// Last-seen WorkshopMode per session — used to detect mode toggles between
	// turns. When the mode changes, the saved CLI session IDs above are dropped
	// (so the new system prompt + tool list actually take effect on the next
	// CLI invocation) and the conversation history is replaced with a
	// synthetic recap so the new agent sees just enough context to continue.
	lastWorkshopModeBySession map[string]string

	// Interactive workshop chat sessions — per-session controller + step registry
	// Key: sessionID, Value: *todo_creation_human.WorkshopChatSession
	workshopChatSessions sync.Map

	// Cron scheduler service for scheduled workflow executions
	scheduler *SchedulerService

	// Background completion loop tracking — prevents multiple loops per session
	completionLoopStarted   map[string]bool
	completionLoopStartedMu sync.Mutex

	toolStatus    map[string]ToolStatus
	enabledTools  map[string][]string // queryID/sessionID -> enabled tool names
	toolStatusMux sync.RWMutex
	mcpConfig     *mcpclient.MCPConfig

	// Background tool discovery
	discoveryRunning       bool
	discoveryMux           sync.RWMutex
	lastDiscovery          time.Time
	discoveryTicker        *time.Ticker
	discoveryFailedServers map[string]string // serverName -> error reason (skipped on subsequent discovery cycles)

	// Per-server install/connection logs
	serverLogs    map[string][]ServerLogEntry
	serverLogsMux sync.RWMutex

	// Logger for structured logging
	logger loggerv2.Logger

	// Bot conversation manager for Slack/Discord/Telegram bot sessions
	botManager *slackservice.BotConversationManager

	// Web simulator connector for testing bot flow without Slack
	webSimulator *slackservice.WebSimulatorConnector
	whatsappSvc  *slackservice.WhatsAppService

	// API token for bearer auth on per-tool endpoints (code execution mode)
	apiToken string
}

// QueryRequest represents an agent query request
type QueryRequest struct {
	Query          string                  `json:"query"`
	Message        string                  `json:"message,omitempty"` // Alias for Query (used by frontend)
	Servers        []string                `json:"servers,omitempty"`
	EnabledServers []string                `json:"enabled_servers,omitempty"`
	SelectedTools  []string                `json:"selected_tools,omitempty"` // Array of "server:tool" strings
	Provider       string                  `json:"provider,omitempty"`
	ModelID        string                  `json:"model_id,omitempty"`
	Temperature    float64                 `json:"temperature,omitempty"`
	MaxTurns       int                     `json:"max_turns,omitempty"`
	AgentMode      string                  `json:"agent_mode,omitempty"`
	LLMConfig      *orchestrator.LLMConfig `json:"llm_config,omitempty"`
	PresetQueryID  string                  `json:"preset_query_id,omitempty"`
	LLMGuidance    string                  `json:"llm_guidance,omitempty"` // LLM guidance message
	// Code execution mode: When enabled, only virtual tools are added to LLM
	// MCP tools are accessed via generated Go code using discover_code_files and write_code
	UseCodeExecutionMode bool `json:"use_code_execution_mode,omitempty"`
	// Execution options from frontend (for workflow execution phase)
	ExecutionOptions *ExecutionOptions `json:"execution_options,omitempty"`
	// Context summarization configuration
	EnableContextSummarization     *bool   `json:"enable_context_summarization,omitempty"`       // Enable context summarization feature (nil = inherit default, true/false = explicit override)
	SummarizeOnTokenThreshold      *bool   `json:"summarize_on_token_threshold,omitempty"`       // Enable token-based summarization trigger (nil = inherit default, true/false = explicit override)
	TokenThresholdPercent          float64 `json:"token_threshold_percent,omitempty"`            // Percentage of context window to trigger summarization (0.0-1.0, default: 0.8 = 80%)
	SummarizeOnFixedTokenThreshold *bool   `json:"summarize_on_fixed_token_threshold,omitempty"` // Enable fixed token-based summarization trigger (nil = inherit default, true/false = explicit override)
	FixedTokenThreshold            int     `json:"fixed_token_threshold,omitempty"`              // Fixed token threshold to trigger summarization (default: 200000 = 200k tokens, matches orchestrator)
	SummaryKeepLastMessages        int     `json:"summary_keep_last_messages,omitempty"`         // Number of recent messages to keep when summarizing (default: 4, matches orchestrator)
	// Context editing configuration
	EnableContextEditing        *bool `json:"enable_context_editing,omitempty"`         // Enable context editing (nil = inherit default, true/false = explicit override)
	ContextEditingThreshold     int   `json:"context_editing_threshold,omitempty"`      // Token threshold for context editing (0 = use default: 100)
	ContextEditingTurnThreshold int   `json:"context_editing_turn_threshold,omitempty"` // Turn age threshold for context editing (0 = use default: 5)
	// Workspace access configuration (legacy field, ignored — workspace is always enabled)
	EnableWorkspaceAccess *bool `json:"enable_workspace_access,omitempty"`
	// Browser automation access configuration
	EnableBrowserAccess *bool `json:"enable_browser_access,omitempty"` // Enable/disable browser automation tool (nil = inherit default, true/false = explicit override)
	// Explicit browser mode from frontend: none|headless|cdp|playwright
	BrowserMode string `json:"browser_mode,omitempty"`
	// Google Workspace access configuration
	EnableGWSAccess *bool `json:"enable_gws_access,omitempty"` // Enable/disable Google Workspace CLI access (nil = inherit default, true/false = explicit override)
	// CDP port for connecting to an existing Chrome browser (local mode only)
	CdpPort *int `json:"cdp_port,omitempty"` // When set and > 0, connect to Chrome via CDP on this port instead of launching headless
	// Image generation configuration
	EnableImageGeneration *bool           `json:"enable_image_generation,omitempty"` // Enable image generation virtual tool
	ImageGenConfig        *ImageGenConfig `json:"image_gen_config,omitempty"`        // Image generation provider configuration
	// Selected skills to include in chat context
	SelectedSkills []string `json:"selected_skills,omitempty"` // Array of skill folder names
	// BotPlatform identifies the chat channel the session is talking through
	// (e.g. "slack", "whatsapp"). Set by the bot manager when wiring a bot
	// session; empty for chat-UI sessions. Drives channel-specific system
	// prompt additions (formatting rules), so bot replies render correctly.
	BotPlatform string `json:"bot_platform,omitempty"`
	// Selected sub-agent templates to make available for delegation
	SelectedSubAgents []string `json:"selected_subagents,omitempty"` // Array of sub-agent template folder names
	// Delegation tier configuration: Maps reasoning levels (high/medium/low) to specific provider/model pairs
	DelegationTierConfig *virtualtools.DelegationTierConfig `json:"delegation_tier_config,omitempty"`
	// Decrypted secrets to inject into agent system prompt
	DecryptedSecrets []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"decrypted_secrets,omitempty"`
	// Selected global secret names to include (if nil/absent, all global secrets are included)
	SelectedGlobalSecrets *[]string `json:"selected_global_secrets,omitempty"`
	// Workspace paths of workflows to inject context for (via # selector in chat)
	WorkflowContextPaths []string `json:"workflow_context_paths,omitempty"`

	// Workflow phase chat: phase ID for running a phase as a conversational chat session
	// When agent_mode is "workflow_phase", this specifies which phase to run (e.g., "planning", "plan-improvement")
	PhaseID string `json:"phase_id,omitempty"`

	// Workspace path passed directly (used by scheduler to bypass preset lookup)
	SelectedFolder string `json:"selected_folder,omitempty"`

	// Triggered by: "manual", "cron" — for tracking execution source
	TriggeredBy string `json:"triggered_by,omitempty"`
	// Auto-notification flag: when true, this is a background agent completion notification
	// (not user-initiated). Backend treats it as a synthetic turn so frontend doesn't block input.
	IsAutoNotification bool `json:"is_auto_notification,omitempty"`
	// Internal: user ID for synthetic turn reconstruction (not from JSON)
	userID string `json:"-"`
}

// ImageGenConfig holds image generation provider configuration
type ImageGenConfig struct {
	Provider string `json:"provider"` // e.g. "vertex"
	ModelID  string `json:"model_id"` // e.g. "imagen-4.0-generate-001"
	APIKey   string `json:"api_key"`  // e.g. GEMINI_API_KEY value (optional; backend falls back to env var)
}

// getCdpPort returns the CDP port from a QueryRequest, or 0 if not set
func getCdpPort(req QueryRequest) int {
	if req.CdpPort != nil {
		return *req.CdpPort
	}
	// If frontend explicitly selected CDP mode but omitted a port, default to 9222.
	// This avoids silently falling back to headless prompt/tool wiring.
	if strings.EqualFold(strings.TrimSpace(req.BrowserMode), "cdp") {
		return 9222
	}
	return 0
}

// getBrowserMode resolves effective browser mode with backward-compatible fallback.
func getBrowserMode(req QueryRequest) string {
	mode := strings.ToLower(strings.TrimSpace(req.BrowserMode))
	switch mode {
	case "none", "headless", "cdp", "playwright":
		return mode
	}

	enableBrowser := req.EnableBrowserAccess != nil && *req.EnableBrowserAccess
	if enableBrowser {
		if getCdpPort(req) > 0 {
			return "cdp"
		}
		return "headless"
	}
	for _, s := range req.EnabledServers {
		if s == "playwright" {
			return "playwright"
		}
	}
	return "none"
}

// resolveWorkflowBrowserSessionID computes the deterministic browser session ID
// for a workflow+group, matching the orchestrator's resolveWorkshopBrowserSessionID.
func resolveWorkflowBrowserSessionID(workspacePath, groupName string) string {
	if groupName == "" {
		groupName = "default-group"
	}
	safeGroupName := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(groupName)
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(workspacePath)))
	_, _ = hasher.Write([]byte("::"))
	_, _ = hasher.Write([]byte(groupName))
	return fmt.Sprintf("workflow-browser-%x-%s", hasher.Sum64(), safeGroupName)
}

// buildChatBrowserConfig resolves the browser configuration from a QueryRequest
// into the standardized BrowserConfig used by BuildBrowserInstructions.
func buildChatBrowserConfig(req QueryRequest) browserinstructions.BrowserConfig {
	cfg := browserinstructions.BrowserConfig{
		CdpPort: getCdpPort(req),
	}
	hasBrowserAccess := req.EnableBrowserAccess != nil && *req.EnableBrowserAccess
	if hasBrowserAccess {
		cfg.HasAgentBrowser = true
	}
	for _, s := range req.EnabledServers {
		switch s {
		case "playwright":
			cfg.HasPlaywright = true
		}
	}
	return cfg
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// QueryResponse represents an agent query response
type QueryResponse struct {
	QueryID   string `json:"query_id"`
	SessionID string `json:"session_id"` // The actual session ID used for conversation history
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// LLMGuidanceRequest represents a request to set LLM guidance for a session
type LLMGuidanceRequest struct {
	SessionID    string `json:"session_id"`
	Guidance     string `json:"guidance"`
	MemoryFolder string `json:"memory_folder,omitempty"` // Optional override for memory folder path
}

// LLMGuidanceResponse represents the response for LLM guidance operations
type LLMGuidanceResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Guidance  string `json:"guidance,omitempty"`
}

// SteerMessageRequest represents a request to inject a user message mid-execution
type SteerMessageRequest struct {
	Message string `json:"message"`
}

// HumanFeedbackRequest represents a request to submit human feedback
type HumanFeedbackRequest struct {
	UniqueID string `json:"unique_id"`
	Response string `json:"response"`
}

// HumanFeedbackResponse represents the response for human feedback operations
type HumanFeedbackResponse struct {
	UniqueID string `json:"unique_id"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

// --- TOOL MANAGEMENT API ---

func init() {
	// Add server command flags
	ServerCmd.Flags().IntP("port", "p", 8000, "Server port")
	ServerCmd.Flags().StringP("host", "H", "0.0.0.0", "Server host")
	ServerCmd.Flags().StringSlice("cors-origins", []string{"*"}, "CORS allowed origins")
	ServerCmd.Flags().String("provider", "bedrock", "LLM provider (bedrock, openai, anthropic)")
	ServerCmd.Flags().String("model", "", "Model ID (uses provider default if empty)")
	ServerCmd.Flags().Float64("temperature", 0.0, "Temperature for LLM")
	ServerCmd.Flags().Int("max-turns", 500, "Maximum conversation turns")
	ServerCmd.Flags().String("mcp-config", "configs/mcp_servers_clean.json", "MCP servers configuration path")

	// Bind flags to viper
	viper.BindPFlags(ServerCmd.Flags())

	ServerCmd.AddCommand(rotateProviderKeysCmd)
}

func runServer(cmd *cobra.Command, args []string) {
	// Load configuration
	config := ServerConfig{
		Port:          viper.GetInt("port"),
		Host:          viper.GetString("host"),
		CORSOrigins:   viper.GetStringSlice("cors-origins"),
		Provider:      viper.GetString("provider"),
		ModelID:       viper.GetString("model"),
		Temperature:   viper.GetFloat64("temperature"),
		MaxTurns:      viper.GetInt("max-turns"),
		MCPConfigPath: viper.GetString("mcp-config"),
	}

	log.Printf("[SERVER DEBUG] Using MCP config file: %s", config.MCPConfigPath)

	// Load .env file for environment variables (OPENAI_API_KEY, etc.)
	// Only load if not already loaded
	if os.Getenv("MCP_ENV_LOADED") == "" {
		if err := godotenv.Load(); err == nil {
			os.Setenv("MCP_ENV_LOADED", "1")
			fmt.Println("[ENV] Loaded .env file for LLM config")
		}
	}

	// Show execution agent LLM config at startup
	agentProvider := os.Getenv("AGENT_PROVIDER")
	if agentProvider == "" {
		agentProvider = "bedrock" // fallback default
	}
	agentModel := os.Getenv("AGENT_MODEL")
	if agentModel == "" {
		agentModel = os.Getenv("BEDROCK_PRIMARY_MODEL") // Use .env configuration
	}
	fmt.Printf("\U0001F916 Agent:   %s | Model: %s\n", agentProvider, agentModel)

	// Apply environment overrides to config (ensure Terraform env vars take precedence)
	if val := os.Getenv("AGENT_PROVIDER"); val != "" {
		config.Provider = val
	}
	// Also apply model override if set (and not just falling back to defaults)
	if agentModel != "" && (os.Getenv("AGENT_MODEL") != "" || os.Getenv("BEDROCK_PRIMARY_MODEL") != "") {
		config.ModelID = agentModel
	}
	// Show cross-provider fallback configuration
	bedrockOpenAIFallback := os.Getenv("BEDROCK_OPENAI_FALLBACK_MODELS")
	if bedrockOpenAIFallback != "" {
		fmt.Printf("🔄 Cross-Provider Fallback: Bedrock → OpenAI (%s)\n", bedrockOpenAIFallback)
	} else {
		fmt.Printf("⚠️  Cross-Provider Fallback: Not configured (set BEDROCK_OPENAI_FALLBACK_MODELS)\n")
	}

	// Validate provider
	llmProvider, err := llm.ValidateProvider(config.Provider)
	if err != nil {
		log.Fatalf("Invalid provider: %v", err)
	}

	// Set default model if not specified
	if config.ModelID == "" {
		config.ModelID = llm.GetDefaultModel(llmProvider)
	}

	// In multi-user mode, AUTH_SECRET must be explicitly set for secure JWT signing.
	// In single-user mode, auth is bypassed entirely so AUTH_SECRET is not needed.
	if IsMultiUserMode() && os.Getenv("AUTH_SECRET") == "" {
		log.Fatal("[AUTH] FATAL: AUTH_SECRET env var must be set when MULTI_USER_MODE=true. Generate a random secret and add it to your deployment configuration.")
	}

	// Clean up stale agent-browser runtime state (dead PID files, sockets)
	// to prevent "CDP response channel closed" errors on first browser use.
	browser.CleanupStaleRuntimeState()

	// Start background reaper: kills browser sessions idle for >15 min so
	// Chrome/daemon processes don't accumulate and exhaust memory.
	browser.StartIdleReaper(15 * time.Minute)

	fmt.Printf("🚀 Starting Streaming API Server\n")
	fmt.Printf("📡 Host: %s:%d\n", config.Host, config.Port)
	fmt.Printf("🤖 Primary Provider: %s | Model: %s\n", config.Provider, config.ModelID)
	// Show tracing configuration
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}
	fmt.Printf("📊 Tracing: %s\n", tracingProvider)

	fmt.Printf("🌐 CORS Origins: %v\n", config.CORSOrigins)
	fmt.Printf("🔒 LLM Config Locked: %v (Env: %s)\n", isGlobalLLMConfigLocked(), os.Getenv("LLM_CONFIG_LOCKED"))

	// Daily ping to keep a Supabase free-tier auth project from auto-pausing.
	// No-op unless AUTH_PROVIDERS includes supabase.
	StartSupabaseKeepalive(context.Background())
	fmt.Printf("📋 Supported Providers: %s\n", os.Getenv("SUPPORTED_LLM_PROVIDERS"))
	fmt.Printf("📁 Config: %s\n", config.MCPConfigPath)

	// Create streaming API server
	configPath := config.MCPConfigPath

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("⚠️  MCP config file not found at %s, initializing empty config...", configPath)

		// Ensure directory exists
		configDir := filepath.Dir(configPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			log.Fatalf("Failed to create config directory: %v", err)
		}

		// Create empty config file
		emptyConfig := &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}
		if err := mcpclient.SaveConfig(configPath, emptyConfig); err != nil {
			log.Fatalf("Failed to create empty MCP config: %v", err)
		}
		log.Printf("✅ Created empty MCP config at %s", configPath)
	}

	mcpConfig, err := mcpclient.LoadConfig(configPath, nil) // Logger not yet available, will be created later
	if err != nil {
		log.Fatalf("Failed to load MCP config: %v", err)
	}

	// Initialize polling system (activity callback will be set after api is created).
	// Keep the backend close to the frontend retention window. Large workflow runs can
	// emit bulky tool events; retaining 10k events per session makes the server process
	// balloon even after the UI trims them.
	maxSessionEvents := 1500
	if raw := strings.TrimSpace(os.Getenv("EVENT_STORE_MAX_EVENTS")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			maxSessionEvents = parsed
		} else {
			log.Printf("⚠️  Invalid EVENT_STORE_MAX_EVENTS=%q; using default %d", raw, maxSessionEvents)
		}
	}
	eventStore := events.NewEventStore(maxSessionEvents)
	log.Printf("📡 EventStore retention: max %d events per session", maxSessionEvents)

	// Initialize the operator-state store (bot connector configs + user
	// secrets) and the global cost ledger.
	chatStore, err := chathistory.NewWorkspaceAPIStore(getWorkspaceAPIURL())
	if err != nil {
		log.Fatalf("Failed to initialize workspace API operator store: %v", err)
	}
	defer chatStore.Close()
	costLedger := costledger.NewLedger(getWorkspaceAPIURL())
	fmt.Printf("💾 Operator store: workspace API (%s)\n", getWorkspaceAPIURL())

	// Initialize Slack service for human feedback
	slackSvc, err := slackservice.InitSlackService()
	if err != nil {
		log.Printf("⚠️  Failed to initialize Slack service: %v (Slack integration will be disabled)", err)
	} else {
		log.Printf("✅ Slack service initialized")
		// Set feedback store function for test connections only
		// Note: For receiving feedback, notification manager handles it
		slackservice.SetFeedbackStoreFuncs(
			func(uniqueID string, message string) error {
				store := virtualtools.GetHumanFeedbackStore()
				if store != nil {
					return store.CreateRequest(uniqueID, message)
				}
				return nil
			},
		)
		// Register Slack service with notification manager
		notificationManager := slackservice.GetNotificationManager()
		if notificationManager != nil && slackSvc != nil {
			notificationManager.RegisterConnector(slackSvc)
			// Set feedback store function so notification manager can update feedback store
			notificationManager.SetFeedbackResponseFunc(
				func(uniqueID string, response string) error {
					store := virtualtools.GetHumanFeedbackStore()
					if store != nil {
						return store.SubmitResponse(uniqueID, response)
					}
					return nil
				},
			)
			log.Printf("✅ Slack service registered with notification manager")
		}
	}

	api := &StreamingAPI{
		config:                       config,
		agentCancelFuncs:             make(map[string]context.CancelFunc),
		workflowOrchestratorContexts: make(map[string]context.CancelFunc),
		activeWorkflowExecutions:     make(map[string]*ActiveWorkflowExecution),
		trackedWorkflowExecutions:    make(map[string]*TrackedWorkflowExecution),
		sessionQueryIDs:              make(map[string][]string),
		workflowObjectives:           make(map[string]string),
		conversationHistory:          make(map[string][]llmtypes.MessageContent),
		chatStore:                    chatStore,
		costLedger:                   costLedger,
		eventStore:                   eventStore,
		provider:                     config.Provider,
		model:                        config.ModelID,
		mcpConfigPath:                configPath,
		temperature:                  config.Temperature,
		workspaceRoot:                "./Tasks",
		toolStatus:                   make(map[string]ToolStatus),
		enabledTools:                 make(map[string][]string),
		mcpConfig:                    mcpConfig,
		serverLogs:                   make(map[string][]ServerLogEntry),
		logger:                       createServerLogger(),
		// Initialize background discovery fields
		discoveryRunning:       false,
		lastDiscovery:          time.Time{},
		discoveryTicker:        nil,
		discoveryFailedServers: make(map[string]string),
		// Initialize active session tracking
		activeSessions: make(map[string]*ActiveSessionInfo),
		// Initialize orchestrator storage
		workflowOrchestrators: make(map[string]orchestrator.Orchestrator),
		// Initialize workflow step ID storage
		workflowStepIDs: make(map[string]string),
		// Initialize background agent infrastructure
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		sessionBusy:               make(map[string]bool),
		pendingCompletions:        make(map[string][]string),
		lastQueryRequests:         make(map[string]QueryRequest),
		sessionWorkspaceFolders:   make(map[string]string),
		sessionAgents:             make(map[string]*agent.LLMAgentWrapper),
		runningAgents:             make(map[string]*mcpagent.Agent),
		completionLoopStarted:     make(map[string]bool),
		claudeCodeSessionIDs:      make(map[string]string),
		geminiSessionIDs:          make(map[string]string),
		geminiProjectDirIDs:       make(map[string]string),
		lastWorkshopModeBySession: make(map[string]string),
		stoppedSessions:           make(map[string]bool),
	}

	// Kill any orphaned browser processes from a previous run.
	// On restart, the in-memory SessionTracker is empty but chromium processes
	// may still be running in the workspace-api container from the previous session.
	go func() {
		workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
		if workspaceAPIURL == "" {
			workspaceAPIURL = "http://127.0.0.1:8081"
		}
		client := browser.NewClient(workspaceAPIURL)
		// Send a kill-all command via workspace-api to clean up any leftover browsers
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.ExecuteCommand(ctx, []string{"--kill-all"}, &browser.ExecuteOptions{Timeout: 15 * time.Second})
		if err != nil {
			// --kill-all may not be supported; fall back to pkill
			log.Printf("[BROWSER_CLEANUP] --kill-all not supported, trying pkill fallback")
			killCmd := "pkill -9 -f 'agent-browser' 2>/dev/null; pkill -9 -f chromium 2>/dev/null; pkill -9 -f 'Google Chrome for Testing' 2>/dev/null; echo 'cleanup done'"
			reqBody := browser.ShellExecuteRequest{Command: killCmd, WorkingDirectory: ".", Timeout: 10}
			jsonBody, _ := json.Marshal(reqBody)
			req, _ := http.NewRequestWithContext(ctx, "POST", workspaceAPIURL+"/api/execute", bytes.NewBuffer(jsonBody))
			if req != nil {
				req.Header.Set("Content-Type", "application/json")
				resp, execErr := http.DefaultClient.Do(req)
				if execErr != nil {
					log.Printf("[BROWSER_CLEANUP] Startup cleanup failed: %v (browsers may still be running)", execErr)
				} else {
					resp.Body.Close()
					log.Printf("[BROWSER_CLEANUP] Startup cleanup: killed orphaned browser processes in workspace-api")
				}
			}
		} else {
			log.Printf("[BROWSER_CLEANUP] Startup cleanup: killed orphaned browser processes via --kill-all")
		}
	}()

	// Generate API token for code execution mode per-tool endpoints
	api.apiToken = executor.GenerateAPIToken()

	// Set env vars for code execution mode (mcpagent reads these as fallback)
	// MCP_API_URL = Docker-reachable URL (for shell commands inside Docker + OpenAPI spec base URLs)
	// MCP_BRIDGE_API_URL = host-reachable URL (for mcpbridge binary running on the host)
	os.Setenv("MCP_API_URL", api.GetCodeExecAPIURL())
	os.Setenv("MCP_BRIDGE_API_URL", api.GetAPIURL())
	os.Setenv("MCP_API_TOKEN", api.apiToken)

	// Load global secrets from GLOBAL_SECRET_* environment variables
	loadGlobalSecrets()

	// Setup routes
	router := mux.NewRouter()

	// CORS middleware
	router.Use(api.corsMiddleware)

	// Auth middleware - applies to all API routes
	// Note: AuthMiddleware handles skipping auth for public endpoints (login, register, health, shared)
	router.Use(AuthMiddleware)

	// API routes
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.Use(api.apiRequestLogMiddleware)

	// Authentication API routes (public - no auth required, handled by AuthMiddleware)
	apiRouter.HandleFunc("/auth/register", api.handleRegister).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/login", api.handleLogin).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/logout", api.handleLogout).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/me", api.handleGetCurrentUser).Methods("GET")
	apiRouter.HandleFunc("/auth/mode", api.handleGetAuthMode).Methods("GET")
	// Multi-provider OAuth routes
	apiRouter.HandleFunc("/auth/start", api.handleAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/callback", api.handleAuthCallback).Methods("GET")

	apiRouter.HandleFunc("/query", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/health", api.handleHealth).Methods("GET")
	apiRouter.HandleFunc("/capabilities", api.handleCapabilities).Methods("GET")
	apiRouter.HandleFunc("/cdp-check", api.handleCdpCheck).Methods("GET")
	apiRouter.HandleFunc("/downloads/chrome-cdp-macOS.zip", api.handleChromeCdpDownload).Methods("GET")
	apiRouter.HandleFunc("/llm-config/defaults", api.handleGetLLMDefaults).Methods("GET")
	apiRouter.HandleFunc("/llm-config/models/metadata", api.handleGetModelMetadata).Methods("GET")
	apiRouter.HandleFunc("/llm-config/azure/deployments", api.handleGetAzureDeployedModels).Methods("POST")
	apiRouter.HandleFunc("/llm-config/validate-key", api.handleValidateAPIKey).Methods("POST")
	apiRouter.HandleFunc("/image-gen/test", api.handleTestImageGen).Methods("POST")
	apiRouter.HandleFunc("/llm-config/delegation-tiers", api.handleGetDelegationTierDefaults).Methods("GET")
	apiRouter.HandleFunc("/session/cancel-turn", api.handleCancelCurrentTurn).Methods("POST")
	apiRouter.HandleFunc("/session/stop", api.handleStopSession).Methods("POST")
	apiRouter.HandleFunc("/session/clear", api.handleClearSession).Methods("POST")

	// Tool management routes (from tools.go)
	apiRouter.HandleFunc("/tools", api.handleGetTools).Methods("GET")
	apiRouter.HandleFunc("/tools/detail", api.handleGetToolDetail).Methods("GET")
	apiRouter.HandleFunc("/tools/enabled", api.handleSetEnabledTools).Methods("POST")
	apiRouter.HandleFunc("/tools/add", api.handleAddServer).Methods("POST")
	apiRouter.HandleFunc("/tools/edit", api.handleEditServer).Methods("POST")
	apiRouter.HandleFunc("/tools/remove", api.handleRemoveServer).Methods("POST")

	// Tool execution APIs - handlers provided by mcpagent/executor library
	// Pass server logger for proper debugging of session registry usage
	executorHandlers := executor.NewExecutorHandlers(api.mcpConfigPath, api.logger)

	// [BROWSER_UPLOAD] Register path transformer on the HTTP executor handler (backup path).
	// The primary interception happens on the Agent itself (see ~line 4615 and ~line 7086),
	// but this covers any direct HTTP /api/mcp/execute calls that bypass the agent.
	// Resolves workspace-relative paths (e.g. "Downloads/file.pdf") to absolute host paths
	// so Playwright MCP can find them — Playwright requires absolute paths for browser_file_upload.
	// Use WorkspaceShellRoot() since Playwright runs inside the Docker container.
	workspaceAbsPath := fsutil.WorkspaceShellRoot()
	log.Printf("[BROWSER_UPLOAD] Registered browser_file_upload transformer, workspace=%s", workspaceAbsPath)
	executorHandlers.SetToolArgTransformer("browser_file_upload", func(args map[string]interface{}) {
		paths, ok := args["paths"].([]interface{})
		if !ok || len(paths) == 0 {
			log.Printf("[BROWSER_UPLOAD] No paths in args or wrong type, skipping transform")
			return
		}
		for i, p := range paths {
			pathStr, ok := p.(string)
			if !ok || pathStr == "" || filepath.IsAbs(pathStr) {
				log.Printf("[BROWSER_UPLOAD] Skipping path[%d]=%q (abs or empty)", i, p)
				continue
			}
			resolved := filepath.Join(workspaceAbsPath, pathStr)
			log.Printf("[BROWSER_UPLOAD] Resolved path[%d]: %q -> %q", i, pathStr, resolved)
			paths[i] = resolved
		}
	})

	apiRouter.HandleFunc("/mcp/execute", executorHandlers.HandleMCPExecute).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/custom/execute", executorHandlers.HandleCustomExecute).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/virtual/execute", executorHandlers.HandleVirtualExecute).Methods("POST", "OPTIONS")

	// Per-tool endpoints for code execution mode (bearer token auth, bypasses JWT)
	// LLM-generated code calls these directly, so they use API token auth instead of JWT.
	//
	// NOTE: The system prompt tool index lists custom tool categories (e.g. workspace_advanced)
	// and virtual tool categories (e.g. workflow) alongside real MCP servers. Claude Code agents
	// call them all via /tools/mcp/{server}/{tool}. The routeMCPRequest helper detects these
	// categories and redirects to the correct handler (custom or virtual).
	customToolCategories := map[string]bool{
		"workspace": true, "workspace_basic": true, "workspace_browser": true,
		"workspace_advanced": true, "workspace_git": true, "workspace_image": true,
		"workspace_image_gen": true, "workspace_image_edit": true, "human": true,
		"workflow": true, "workflow_creator": true,
		"llm_config_tools": true, "secret_tools": true, "skill_tools": true,
		"mcp_server_tools": true, "activity_status": true,
		// Tools registered by guidance.RegisterGuidanceTool — namespaced as
		// "auto_improvement" in the tool index. Without this entry, the LLM's
		// curl call to /tools/mcp/auto_improvement/get_workflow_command_guidance
		// falls through to MCP-server lookup and returns "server not configured".
		"auto_improvement": true,
	}
	virtualToolCategories := map[string]bool{
		"memory": true,
	}
	routeMCPRequest := func(w http.ResponseWriter, r *http.Request, server, tool string) {
		// Normalize: hyphens to underscores for category lookup
		normalized := strings.ReplaceAll(server, "-", "_")
		if customToolCategories[normalized] {
			log.Printf("[ROUTE] Redirecting /tools/mcp/%s/%s → custom tool handler", server, tool)
			executorHandlers.HandlePerToolCustomRequest(w, r, tool)
			return
		}
		if virtualToolCategories[normalized] {
			log.Printf("[ROUTE] Redirecting /tools/mcp/%s/%s → virtual tool handler", server, tool)
			executorHandlers.HandlePerToolVirtualRequest(w, r, tool)
			return
		}
		executorHandlers.HandlePerToolMCPRequest(w, r, server, tool)
	}

	toolsRouter := router.PathPrefix("/tools").Subrouter()
	toolsRouter.Use(executor.AuthMiddleware(api.apiToken))
	toolsRouter.HandleFunc("/mcp/{server}/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		routeMCPRequest(w, r, vars["server"], vars["tool"])
	}).Methods("POST", "OPTIONS")
	toolsRouter.HandleFunc("/custom/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		log.Printf("[GLOBAL_ROUTE_DEBUG] Global custom tool request: tool=%s url=%s x-session-id=%s", vars["tool"], r.URL.Path, r.Header.Get("X-Session-ID"))
		executorHandlers.HandlePerToolCustomRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")
	toolsRouter.HandleFunc("/virtual/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		executorHandlers.HandlePerToolVirtualRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")

	// Session-scoped per-tool endpoints: /s/{session_id}/tools/...
	// These routes bake the session_id into the URL path, so the LLM-generated code
	// doesn't need to explicitly include session_id in request bodies.
	// The session_id is extracted from the path and injected as X-Session-ID header,
	// which the per-tool handler reads as a fallback when body session_id is empty.
	sessionToolsRouter := router.PathPrefix("/s/{session_id}/tools").Subrouter()
	sessionToolsRouter.Use(executor.AuthMiddleware(api.apiToken))
	sessionToolsRouter.HandleFunc("/mcp/{server}/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sid := vars["session_id"]
		r.Header.Set("X-Session-ID", sid)
		// MCP-style URLs can be redirected to custom workspace tools. Those
		// tools resolve folder guards from context, so mirror /tools/custom.
		ctx := context.WithValue(r.Context(), common.ChatSessionIDKey, sid)
		routeMCPRequest(w, r.WithContext(ctx), vars["server"], vars["tool"])
	}).Methods("POST", "OPTIONS")
	sessionToolsRouter.HandleFunc("/custom/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sid := vars["session_id"]
		tool := vars["tool"]
		log.Printf("[SESSION_ROUTE_DEBUG] Session-scoped custom tool request: session=%s tool=%s url=%s", sid, tool, r.URL.Path)
		r.Header.Set("X-Session-ID", sid)
		// Inject ChatSessionIDKey so execute_shell_command can look up
		// the session's working directory and folder guard from the global map.
		ctx := context.WithValue(r.Context(), common.ChatSessionIDKey, sid)
		executorHandlers.HandlePerToolCustomRequest(w, r.WithContext(ctx), tool)
	}).Methods("POST", "OPTIONS")
	sessionToolsRouter.HandleFunc("/virtual/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		r.Header.Set("X-Session-ID", vars["session_id"])
		executorHandlers.HandlePerToolVirtualRequest(w, r, vars["tool"])
	}).Methods("POST", "OPTIONS")

	// MCP Config API routes (from mcp_config_routes.go)
	apiRouter.HandleFunc("/mcp-config", api.handleGetMCPConfig).Methods("GET")
	apiRouter.HandleFunc("/mcp-config", api.handleSaveMCPConfig).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/discover", api.handleDiscoverServers).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/status", api.handleGetMCPConfigStatus).Methods("GET")
	apiRouter.HandleFunc("/mcp-config/logs", api.handleGetServerLogs).Methods("GET")

	// Secrets encryption API routes (from secrets_routes.go)
	apiRouter.HandleFunc("/secrets/encrypt", api.handleEncryptSecret).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/secrets/decrypt", api.handleDecryptSecret).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/secrets/global", api.handleGetGlobalSecrets).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/secrets/store", api.handleStoreUserSecret).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/secrets/store/{name}", api.handleDeleteUserSecret).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/secrets/stored", api.handleListStoredSecrets).Methods("GET", "OPTIONS")

	// Provider API keys (encrypted file storage for scheduled runs)
	apiRouter.HandleFunc("/provider-keys", api.handleSaveProviderKeys).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/provider-keys", api.handleLoadProviderKeys).Methods("GET", "OPTIONS")

	// Published LLMs (workspace-backed JSON storage)
	apiRouter.HandleFunc("/published-llms", api.handleSavePublishedLLMs).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/published-llms", api.handleLoadPublishedLLMs).Methods("GET", "OPTIONS")

	// Delegation tier config (plain JSON file storage, shared by chat and bot connector)
	apiRouter.HandleFunc("/delegation-tier-config", api.handleSaveDelegationTierConfig).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/delegation-tier-config", api.handleLoadDelegationTierConfig).Methods("GET", "OPTIONS")

	// OAuth API routes (from oauth_routes.go)
	apiRouter.HandleFunc("/oauth/start", api.handleOAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/oauth/callback", api.handleOAuthCallback).Methods("GET")
	apiRouter.HandleFunc("/oauth/status", api.handleOAuthStatus).Methods("GET")
	apiRouter.HandleFunc("/oauth/logout", api.handleOAuthLogout).Methods("POST", "OPTIONS")

	// Observer APIs removed - events are now stored by sessionID, no observers needed

	// Browser session tracking API
	apiRouter.HandleFunc("/browser/sessions", api.handleGetBrowserSessions).Methods("GET")

	// Active Session API routes (from polling.go)
	apiRouter.HandleFunc("/sessions/active", api.handleGetActiveSessions).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events", api.handleGetSessionEvents).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events/stream", api.handleSSEStream).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/reconnect", api.handleReconnectSession).Methods("POST")
	apiRouter.HandleFunc("/sessions/{session_id}/status", api.handleGetSessionStatus).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/execution-tree", api.handleGetSessionExecutionTree).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/dismiss", api.handleDismissSession).Methods("POST", "OPTIONS")

	// LLM Guidance API routes
	apiRouter.HandleFunc("/sessions/{session_id}/llm-guidance", api.handleSetLLMGuidance).Methods("POST", "OPTIONS")

	// Steer message API route - inject user message mid-execution
	apiRouter.HandleFunc("/sessions/{session_id}/steer", api.handleSteerMessage).Methods("POST", "OPTIONS")

	// Context Summarization API routes
	apiRouter.HandleFunc("/sessions/{session_id}/summarize", api.handleSummarizeConversation).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/sessions/{session_id}/compact", api.handleCompactContext).Methods("POST", "OPTIONS")

	// Human Feedback API
	apiRouter.HandleFunc("/human-feedback/submit", api.handleSubmitHumanFeedback).Methods("POST", "OPTIONS")

	// Workflow running-session API (decoupled from chat session storage).
	apiRouter.HandleFunc("/workflow/running", api.handleListRunningWorkflows).Methods("GET")
	apiRouter.HandleFunc("/workflow/running/{session_id}", api.handleGetRunningWorkflow).Methods("GET")
	apiRouter.HandleFunc("/workflow/running/{session_id}", api.handleUpdateRunningWorkflow).Methods("PATCH", "OPTIONS")

	// Global cost ledger summary.
	apiRouter.HandleFunc("/cost/summary", api.handleCostSummary).Methods("GET")

	// Chat history (read-only, persisted to workspace)
	ChatHistoryRoutes(router, api)

	// Slack Feedback API routes
	SlackFeedbackRoutes(router, api)

	// Per-user notification preferences (Slack channel, WhatsApp number)
	NotificationPreferencesRoutes(router)

	// Initialize Bot Conversation Manager
	workspaceURL := os.Getenv("WORKSPACE_API_URL")
	if workspaceURL == "" {
		workspaceURL = "http://127.0.0.1:8081"
	}
	botManager := slackservice.NewBotConversationManager(chatStore, configPath, workspaceURL)
	botManager.SetEventSubscriber(NewBotEventSubscriberAdapter(eventStore))
	// Bot sessions use ONLY delegation tier config from DB for LLM selection — no server defaults needed
	api.botManager = botManager
	// Wire startSessionInternal after api is created (closure captures api)
	botManager.SetStartSessionFunc(api.startSessionInternal)
	botManager.SetFollowUpFunc(api.sendFollowUpInternal)
	botManager.SetRunningWorkflowsFunc(func(userID string) []slackservice.BotRunningWorkflow {
		running := api.listRunningWorkflowExecutions(userID)
		out := make([]slackservice.BotRunningWorkflow, 0, len(running))
		for _, wf := range running {
			label := strings.TrimSpace(wf.PresetName)
			if label == "" && wf.WorkspacePath != "" {
				label = workflowNameFromWorkspacePath(wf.WorkspacePath)
			}
			out = append(out, slackservice.BotRunningWorkflow{
				WorkflowLabel:    label,
				WorkspacePath:    wf.WorkspacePath,
				Status:           wf.Status,
				CurrentStepTitle: wf.CurrentStepTitle,
				PhaseName:        wf.PhaseName,
				Title:            wf.Title,
				SessionID:        wf.SessionID,
				StartedAt:        wf.StartedAt,
			})
		}
		return out
	})
	// Install a chat injector so workflows launched from a builder chat session
	// can route human_input questions back as a synthetic turn on that session
	// (instead of the blocking popup UI). The builder agent receives the
	// question, decides whether to answer it from its own context or ask the
	// user, and resolves the workflow via submit_human_answer.
	virtualtools.SetChatInjector(func(ctx context.Context, sessionID, userID, message string) error {
		api.executeSyntheticTurn(sessionID, message)
		return nil
	})
	// Install the bot manager as the spawn listener. Any tool that registers a
	// parent chat (run_workflow, run_step, run_full_workflow, …) will now
	// automatically mirror its background session's agent messages into the
	// parent's Slack thread — no per-tool hooks required.
	virtualtools.SetSpawnListener(botManager)
	botManager.SetUserSecretsLoader(func(ctx context.Context, userID string) ([]slackservice.DecryptedSecret, error) {
		stored, err := chatStore.ListUserSecrets(ctx, userID)
		if err != nil {
			return nil, err
		}
		var result []slackservice.DecryptedSecret
		for _, s := range stored {
			plaintext, err := decryptSecretValue(s.EncryptedValue, userID)
			if err != nil {
				log.Printf("[SECRETS] Failed to decrypt stored secret %q for user %s: %v", s.Name, userID, err)
				continue // skip broken secrets
			}
			result = append(result, slackservice.DecryptedSecret{Name: s.Name, Value: plaintext})
		}
		return result, nil
	})

	// Wire bot session checker for human feedback (skip 2-min delay for bot sessions)
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if feedbackStore != nil {
		feedbackStore.SetBotSessionChecker(func(sessionID string) bool {
			return botManager.IsBotSession(sessionID)
		})
	}

	// Register Slack as a bot connector if bot_mode is enabled
	if slackSvc != nil {
		botConfig, _ := chatStore.GetBotConnectorConfig(context.Background(), "slack")
		if botConfig != nil && botConfig.BotMode {
			botManager.RegisterConnector(slackSvc)
			slackSvc.StartListening(context.Background())
			log.Printf("✅ Slack bot mode enabled")
		}
	}

	// Register web simulator connector (always available, no config needed)
	webSimulator := slackservice.NewWebSimulatorConnector()
	botManager.RegisterConnector(webSimulator)
	api.webSimulator = webSimulator
	log.Printf("✅ Web bot simulator enabled")

	// Register WhatsApp connector unless explicitly disabled. Pairing is a
	// one-time QR scan; the session DB persists state between restarts.
	// Set WHATSAPP_ENABLED=false to disable and optionally WHATSAPP_SESSION_DB
	// to override the default session DB path.
	//
	// DB usage note: this server otherwise avoids databases and persists to
	// workspace/ files only. WhatsApp is an intentional exception because
	// whatsmeow needs a transactional SQLite store for its Signal-protocol
	// keys (identity, sessions, prekeys). The file is agent-local — not
	// shared infra, not replicated across nodes — so it behaves more like a
	// protocol-state cache than a "database" in the architectural sense.
	// Deleting the file and re-pairing via QR fully restores functionality.
	whatsappEnabled := strings.ToLower(strings.TrimSpace(os.Getenv("WHATSAPP_ENABLED")))
	if whatsappEnabled != "false" && whatsappEnabled != "0" {
		dbPath := os.Getenv("WHATSAPP_SESSION_DB")
		if dbPath == "" {
			dbPath = filepath.Join(fsutil.WorkspaceDocsRoot(), "config", "whatsapp-session.db")
		}
		whatsappSvc := slackservice.NewWhatsAppService(dbPath)
		botManager.RegisterConnector(whatsappSvc)
		api.whatsappSvc = whatsappSvc
		if err := whatsappSvc.StartListening(context.Background()); err != nil {
			log.Printf("❌ WhatsApp service failed to start: %v", err)
		} else {
			log.Printf("✅ WhatsApp bot mode enabled (db=%s)", dbPath)
		}
	}

	// Register bot routes
	BotRoutes(router, api)
	BotSimulatorRoutes(router, api)
	if api.whatsappSvc != nil {
		WhatsAppRoutes(router, api.whatsappSvc)
	}

	// Set activity callback for event store to update session LastActivity when events are added
	eventStore.SetActivityCallback(func(sessionID string) {
		api.updateSessionActivity(sessionID)
	})

	// Start background cleanup goroutine to mark inactive sessions (10 minute timeout)
	go api.cleanupInactiveSessions()

	// Initialize and start the cron scheduler
	// Set SCHEDULER_ENABLED=false in .env to disable on secondary machines sharing the same workspace files.
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	schedulerSvc := NewSchedulerService(api)
	api.scheduler = schedulerSvc
	if os.Getenv("SCHEDULER_ENABLED") == "false" {
		log.Printf("[SCHEDULER] Disabled via SCHEDULER_ENABLED=false — skipping cron execution on this machine")
	} else {
		go func() {
			if err := schedulerSvc.Start(schedulerCtx); err != nil {
				log.Printf("[SCHEDULER] Error: %v", err)
			}
		}()
	}

	// Register scheduler routes
	SchedulerRoutes(router, schedulerSvc)

	// Workflow API routes
	apiRouter.HandleFunc("/workflow/create", api.handleCreateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/status", api.handleGetWorkflowStatus).Methods("GET")
	apiRouter.HandleFunc("/workflow/update", api.handleUpdateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/constants", orchtypes.HandleWorkflowConstants).Methods("GET")
	apiRouter.HandleFunc("/workflow/active-executions", api.handleGetActiveExecutions).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/builder-session", api.handleGetWorkflowBuilderSession).Methods("GET", "OPTIONS")

	// Employee API routes (in employee_routes.go)
	EmployeeRoutes(apiRouter)

	// Workspace API reverse proxy (auth-protected) — frontend calls /api/wp/* instead of /workspace/*
	apiRouter.PathPrefix("/wp/").Handler(workspaceProxyHandler())

	// Consolidated workspace state endpoint (NEW - loads everything in one call)
	apiRouter.HandleFunc("/workspace/state", api.handleLoadWorkspaceState).Methods("GET", "OPTIONS")

	// Legacy individual endpoints (kept for backward compatibility)
	apiRouter.HandleFunc("/workflow/run-folders", api.handleGetRunFolders).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/run-folder", api.handleCreateRunFolder).Methods("POST", "OPTIONS")
	// /workflow/progress endpoint removed — steps_done.json progress tracking no longer consumed by frontend
	apiRouter.HandleFunc("/workflow/run-folder", api.handleDeleteRunFolder).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflow/learnings", api.handleDeleteStepLearnings).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflow/learnings/all", api.handleGetAllStepLearnings).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/variable-groups", api.handleGetVariableGroups).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/variable-groups", api.handleUpdateVariableGroups).Methods("POST", "PUT", "OPTIONS")
	apiRouter.HandleFunc("/workflow/logs", api.handleGetExecutionLogs).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/logs/file", api.handleGetLogFile).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/costs", api.handleGetCosts).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/evaluation-reports", api.handleGetEvaluationReports).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/review-data", api.handleGetWorkflowReviewData).Methods("GET", "OPTIONS")

	// Auto-improvement framework — see docs/workflow/auto_improvement_framework.md
	apiRouter.HandleFunc("/workflow/eval-trajectory", api.handleGetEvalTrajectory).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/metrics", api.handleGetMetrics).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/metrics-history", api.handleGetMetricsHistory).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/builder-doc", api.handleGetBuilderDoc).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/builder-doc-archives", api.handleGetBuilderDocArchives).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/framework-health", api.handleGetFrameworkHealth).Methods("GET", "OPTIONS")

	// Plan and Step Config API routes
	apiRouter.HandleFunc("/workflow/plan/update-step", api.handleUpdatePlanStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/update-step-config", api.handleUpdateStepConfig).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/batch-update-steps", api.handleBatchUpdateSteps).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/delete-step", api.handleDeleteStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/add-step", api.handleAddStep).Methods("POST", "OPTIONS")
	// Dynamic report system (docs/workflow/persistent_stores_design.md section 2).
	// No backend wrappers — the frontend ReportViewer reads reports/report_plan.json
	// and db/*.json / knowledgebase/*.json directly via the workspace service's
	// /api/documents/{path} endpoint (agentApi.getPlannerFileContent).

	// Workflow Version API routes
	apiRouter.HandleFunc("/workflow/versions", api.handleListVersions).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/versions/publish", api.handlePublishVersion).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/versions/revert", api.handleRevertVersion).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/versions", api.handleDeleteVersion).Methods("DELETE", "OPTIONS")

	// Manifest-backed workflow API routes (file-backed workflow definitions)
	apiRouter.HandleFunc("/workflows/summary", api.handleGetWorkflowsSummary).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflows/overview", api.handleGetWorkflowsOverview).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflows/manifests", api.handleListWorkflowManifests).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflows/manifest", api.handleGetWorkflowManifest).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflows/manifest", api.handleCreateWorkflowManifest).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflows/manifest", api.handleUpdateWorkflowManifest).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/workflows/manifest", api.handleDeleteWorkflowManifest).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflows/folder", api.handleDeleteWorkflowFolder).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflows/manifest/duplicate", api.handleDuplicateWorkflowManifest).Methods("POST", "OPTIONS")

	// Skills API routes (from skill_routes.go)
	RegisterSkillRoutes(apiRouter, api)

	// Note: System skills sync runs inside the workspace Docker container (workspace/skill_sync.go)
	// The backend server only proxies skill API calls to the workspace.

	// Sub-agent template API routes (from subagent_routes.go)
	RegisterSubAgentRoutes(apiRouter, api)

	// User-defined command routes (from command_routes.go)
	RegisterCommandRoutes(apiRouter, api)

	// Public file sharing routes — filepath passed as base64 query param
	apiRouter.HandleFunc("/public/file", api.handlePublicFile).Methods("GET")
	apiRouter.HandleFunc("/public/folder", api.handlePublicFolder).Methods("GET")
	apiRouter.HandleFunc("/public/folder/download", api.handlePublicFolderDownload).Methods("GET")

	// pprof routes for profiling (must be before static file serving)
	router.PathPrefix("/debug/pprof/").Handler(http.DefaultServeMux)

	// Pre-bind listener so we can support dynamic port (port 0) and report the actual port
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.Host, config.Port))
	if err != nil {
		log.Fatalf("Failed to listen on %s:%d: %v", config.Host, config.Port, err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	// Dynamically serve runtime-config.js so the frontend learns the real ports.
	// In packaged/desktop mode ports are dynamic (--port 0), so the static file's
	// hardcoded values are wrong. Serve same-origin URLs for the agent API and
	// the workspace URL passed via WORKSPACE_API_URL env var.
	router.HandleFunc("/runtime-config.js", func(w http.ResponseWriter, r *http.Request) {
		workspaceURL := os.Getenv("WORKSPACE_API_URL")
		if workspaceURL == "" {
			workspaceURL = fmt.Sprintf("http://localhost:%d", actualPort)
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "window.__APP_RUNTIME_CONFIG__ = {\n  apiBaseUrl: \"http://localhost:%d\",\n  workspaceApiBaseUrl: %q\n};\n", actualPort, workspaceURL)
	}).Methods("GET")

	// Static file serving (for frontend)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	// Create HTTP server
	srv := &http.Server{
		WriteTimeout: 0,                 // No write timeout — long-running tool calls (sub-agents) can take 30+ minutes
		ReadTimeout:  time.Second * 30,  // Read timeout for incoming requests
		IdleTimeout:  time.Second * 300, // 5 min idle timeout to prevent early closes during long queries
		Handler:      router,
	}

	// Initialize tool cache BEFORE starting HTTP server so the first getTools()
	// request from the frontend gets real data instead of an empty list.
	fmt.Printf("🔄 Initializing tool cache on server startup...\n")
	api.initializeToolCache()

	// Sync system skills (skill-creator, agent-browser, etc.) in background
	go func() {
		syncCtx, syncCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer syncCancel()
		workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
		if workspaceAPIURL == "" {
			workspaceAPIURL = "http://127.0.0.1:8081"
		}
		installed, errs := todo_creation_human.SyncSystemSkills(syncCtx, workspaceAPIURL)
		if len(errs) > 0 {
			for _, e := range errs {
				log.Printf("[SKILLS] %s", e)
			}
		}
		if installed > 0 {
			log.Printf("[SKILLS] ✅ Installed %d system skills on startup", installed)
		}
	}()

	// Start server in a goroutine
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	fmt.Printf("✅ Server started on %s:%d\n", config.Host, actualPort)
	fmt.Printf("DynamicPort: %d\n", actualPort)
	fmt.Printf("🔗 API endpoint: http://%s:%d/api/query\n", config.Host, actualPort)
	fmt.Printf("📡 Polling API: http://%s:%d/api/sessions/{session_id}/events\n", config.Host, actualPort)

	// Wait for interrupt signal to gracefully shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c

	fmt.Println("\n🛑 Shutting down server...")

	// Stop background discovery
	fmt.Println("⏹️ Stopping background tool discovery...")
	api.stopPeriodicRefresh()

	// Close all MCP session connections to prevent orphaned subprocesses
	fmt.Println("🧹 Closing all MCP sessions...")
	mcpagent.CloseAllSessions()

	// Kill all active browser daemons and Chrome processes so they don't linger
	fmt.Println("🧹 Killing all browser sessions...")
	browser.KillAllTrackedSessions()

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Shutdown server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("✅ Server shutdown complete")
}

// GetAPIURL returns the base URL for the API server
// It handles replacing 0.0.0.0 with 127.0.0.1 for local loopback calls
func (api *StreamingAPI) GetAPIURL() string {
	host := api.config.Host
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, api.config.Port)
}

// GetCodeExecAPIURL returns the API URL as seen from wherever shell commands execute.
// In Docker mode, shell commands run inside the workspace-api container and need
// host.docker.internal to reach the Go server on the host.
// In native mode, shell commands run directly on the host, so they use 127.0.0.1.
func (api *StreamingAPI) GetCodeExecAPIURL() string {
	if common.IsNativeWorkspace() {
		return fmt.Sprintf("http://127.0.0.1:%d", api.config.Port)
	}

	wsURL := getWorkspaceAPIURL()
	// If workspace API points to localhost (Docker-mapped port), shell commands
	// run inside Docker and need host.docker.internal to reach the host
	if strings.Contains(wsURL, "localhost") || strings.Contains(wsURL, "127.0.0.1") {
		return fmt.Sprintf("http://host.docker.internal:%d", api.config.Port)
	}
	// In Docker Compose networking, use the Go server's service name or host
	return api.GetAPIURL()
}

// mergeGlobalSecrets prepends global secrets to user-supplied secrets.
// buildModeChangeRecap walks the prior conversation history and returns a
// single synthetic user-message string the new mode's agent can read as
// "this is what just happened in the previous mode." Sliced to roughly the
// last `maxChars` characters so the recap doesn't blow up the new context.
//
// Used when the user toggles workshop mode mid-session: we drop the CLI
// session IDs (so the fresh prompt + tool allow-list take effect) and
// replace the agent-replay history with this recap. Plain prose, no tool
// calls — the new agent shouldn't be tempted to mimic tool calls that may
// not be in its allow-list.
func buildModeChangeRecap(history []llmtypes.MessageContent, prevMode, newMode string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 16000 // ~4000 tokens at 4 chars/token, conservative
	}
	// Walk in reverse so we keep the most-recent turns when slicing. Skip
	// system messages (the prior system prompt no longer applies) and
	// keep tool/AI/user text content.
	var lines []string
	totalChars := 0
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		role := "User"
		switch msg.Role {
		case llmtypes.ChatMessageTypeAI:
			role = "Agent"
		case llmtypes.ChatMessageTypeSystem:
			continue // stale prompt; not worth replaying
		case llmtypes.ChatMessageTypeTool:
			role = "Tool"
		}
		var textParts []string
		for _, part := range msg.Parts {
			if t, ok := part.(llmtypes.TextContent); ok && t.Text != "" {
				textParts = append(textParts, t.Text)
			}
		}
		if len(textParts) == 0 {
			continue
		}
		body := strings.Join(textParts, " ")
		// Trim per-message body so a single huge AI turn doesn't eat the budget.
		if len(body) > 2000 {
			body = body[:2000] + "…"
		}
		line := role + ": " + body
		if totalChars+len(line) > maxChars && len(lines) > 0 {
			break
		}
		lines = append(lines, line)
		totalChars += len(line)
	}
	// Reverse back to chronological order.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	if len(lines) == 0 {
		return fmt.Sprintf("[CONTEXT] You are continuing a chat session that just switched workshop mode from %q to %q. There's no prior conversation context. Proceed with the user's next message in the new mode.", prevMode, newMode)
	}
	return fmt.Sprintf(
		"[CONTEXT FROM PREVIOUS MODE]\nThe user was working in %q mode and has now switched to %q mode. The current system prompt and your tool allow-list reflect %q mode — the previous mode's tools are no longer available, so don't try to call them. The recent conversation summary follows; treat it as background, not as instructions.\n\n%s\n[/CONTEXT]\n\nNow respond to the user's next message in %q mode.",
		prevMode, newMode, newMode,
		strings.Join(lines, "\n\n"),
		newMode,
	)
}

func latestAssistantTextFromHistory(history []llmtypes.MessageContent) string {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		var textParts []string
		for _, part := range msg.Parts {
			if t, ok := part.(llmtypes.TextContent); ok && strings.TrimSpace(t.Text) != "" {
				textParts = append(textParts, t.Text)
			}
		}
		if len(textParts) > 0 {
			return strings.TrimSpace(strings.Join(textParts, "\n"))
		}
	}
	return ""
}

// User secrets take priority on name collision.
// If selectedGlobalNames is non-nil, only global secrets whose name is in the list are included.
func mergeGlobalSecrets(userSecrets []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}, selectedGlobalNames *[]string) []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
} {
	globals := getGlobalSecrets()
	if len(globals) == 0 {
		return userSecrets
	}
	// Build filter set from selected global names (nil = include all)
	var allowedGlobals map[string]bool
	if selectedGlobalNames != nil {
		allowedGlobals = make(map[string]bool, len(*selectedGlobalNames))
		for _, name := range *selectedGlobalNames {
			allowedGlobals[name] = true
		}
	}
	// Build a set of user-supplied secret names for dedup
	userNames := make(map[string]bool, len(userSecrets))
	for _, s := range userSecrets {
		userNames[s.Name] = true
	}
	// Prepend globals that don't collide with user secrets and are in the allowed set
	var merged []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	for _, g := range globals {
		if userNames[g.Name] {
			continue
		}
		if allowedGlobals != nil && !allowedGlobals[g.Name] {
			continue
		}
		merged = append(merged, struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: g.Name, Value: g.Value})
	}
	merged = append(merged, userSecrets...)
	return merged
}

// loadSelectedUserSecrets decrypts the named user-stored secrets from the
// filesystem chat store. Workflow manifests store only selected secret names,
// so runtime must rehydrate the actual values server-side instead of relying
// on stale request payloads.
func (api *StreamingAPI) loadSelectedUserSecrets(ctx context.Context, userID string, selectedNames []string) []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
} {
	if userID == "" || len(selectedNames) == 0 {
		return nil
	}

	stored, err := api.chatStore.ListUserSecrets(ctx, userID)
	if err != nil {
		log.Printf("[SECRETS] Failed to list stored user secrets for %s: %v", userID, err)
		return nil
	}

	selectedSet := make(map[string]bool, len(selectedNames))
	for _, name := range selectedNames {
		selectedSet[name] = true
	}

	// Track which selected names were actually resolved so we can surface orphans.
	// An orphan is a name attached to the workflow with no value in the user store
	// and no matching GLOBAL_SECRET_* env var — runtime would silently set
	// $SECRET_<NAME> to empty, masking downstream failures.
	resolved := make(map[string]bool, len(selectedNames))

	var result []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	for _, s := range stored {
		if !selectedSet[s.Name] {
			continue
		}
		plaintext, err := decryptSecretValue(s.EncryptedValue, userID)
		if err != nil {
			log.Printf("[SECRETS] Failed to decrypt stored secret %q for user %s: %v", s.Name, userID, err)
			continue
		}
		result = append(result, struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: s.Name, Value: plaintext})
		resolved[s.Name] = true
	}

	// Also treat globals as resolved — mergeGlobalSecrets layers these in separately.
	for _, gs := range getGlobalSecrets() {
		if selectedSet[gs.Name] {
			resolved[gs.Name] = true
		}
	}

	var orphans []string
	for _, name := range selectedNames {
		if !resolved[name] {
			orphans = append(orphans, name)
		}
	}
	if len(orphans) > 0 {
		log.Printf("[SECRETS] ⚠️  Workflow attaches secret name(s) with no stored value for user %s: %v — $SECRET_<NAME> will be EMPTY at runtime. Store a value with set_user_secret or detach via update_workflow_config(remove_secrets=[...]).", userID, orphans)
	}

	return result
}

// CORS middleware
func (api *StreamingAPI) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, allowed := range api.config.CORSOrigins {
			if allowed == "*" || allowed == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Session-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

var apiRequestsInFlight int64

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusCapturingResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *statusCapturingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusCapturingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (api *StreamingAPI) apiRequestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldLogAPIRequests() {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		inFlight := atomic.AddInt64(&apiRequestsInFlight, 1)
		recorder := &statusCapturingResponseWriter{ResponseWriter: w}

		log.Printf("[API] --> %s %s in_flight=%d", r.Method, requestLogPath(r), inFlight)
		defer func() {
			remaining := atomic.AddInt64(&apiRequestsInFlight, -1)
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			log.Printf("[API] <-- %s %s status=%d bytes=%d duration=%s in_flight=%d",
				r.Method,
				requestLogPath(r),
				status,
				recorder.bytes,
				time.Since(start).Round(time.Millisecond),
				remaining,
			)
		}()

		next.ServeHTTP(recorder, r)
	})
}

func shouldLogAPIRequests() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("API_REQUEST_LOG")))
	return value != "false" && value != "0" && value != "off"
}

func requestLogPath(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + r.URL.RawQuery
}

// Health check endpoint
func (api *StreamingAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Get current tracing provider
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"time":    time.Now(),
		"version": llmtypes.VERSION,
		"config": map[string]interface{}{"provider": api.config.Provider,
			"model":            api.config.ModelID,
			"temperature":      api.config.Temperature,
			"max_turns":        api.config.MaxTurns,
			"tracing_provider": tracingProvider,
		},
	})
}

// handlePublicFile serves workspace files via a shareable URL.
// GET /api/public/file?path=<base64-encoded-filepath>
func (api *StreamingAPI) handlePublicFile(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	// Decode base64 filepath
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[PUBLIC-FILE] Failed to decode base64 path: %s, error: %v", encoded, err)
			http.Error(w, "invalid file path encoding", http.StatusBadRequest)
			return
		}
	}
	filePath := string(decoded)

	// Use uid from query param (owner's ID for cross-user sharing), fall back to auth context
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
	log.Printf("[PUBLIC-FILE] Serving file: %s for user: %s", filePath, uid)

	// URL-encode each path segment for the workspace API
	segments := strings.Split(filePath, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	encodedPath := strings.Join(segments, "/")

	wsURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "/raw"
	log.Printf("[PUBLIC-FILE] Proxying to workspace: %s", wsURL)

	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Failed to create request: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-User-ID", uid)

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FILE] Workspace returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Proxy content-type and body — force inline display (no download)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Disposition", "inline")
	io.Copy(w, resp.Body)
}

// handlePublicFolder lists workspace folder contents via a shareable URL.
// GET /api/public/folder?path=<base64-encoded-folderpath>
func (api *StreamingAPI) handlePublicFolder(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[PUBLIC-FOLDER] Failed to decode base64 path: %s, error: %v", encoded, err)
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	folderPath := string(decoded)

	// Use uid from query param (owner's ID for cross-user sharing), fall back to auth context
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
	log.Printf("[PUBLIC-FOLDER] Listing folder: %s for user: %s", folderPath, uid)

	wsURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	q := req.URL.Query()
	q.Set("folder", folderPath)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-User-ID", uid)

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FOLDER] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handlePublicFolderDownload exports a shared folder as a ZIP download.
// GET /api/public/folder/download?path=<base64-encoded-folderpath>&uid=<owner-id>
func (api *StreamingAPI) handlePublicFolderDownload(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	folderPath := string(decoded)

	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
	log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Exporting folder: %s for user: %s", folderPath, uid)

	// Proxy to workspace export endpoint
	wsURL := getWorkspaceAPIURL() + "/api/workspace/export"
	body, _ := json.Marshal(map[string]string{"workspace_path": folderPath})
	req, err := http.NewRequestWithContext(r.Context(), "POST", wsURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uid)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Workspace returned %d: %s", resp.StatusCode, string(respBody))
		http.Error(w, "export failed", resp.StatusCode)
		return
	}

	// Proxy ZIP response headers and body
	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	io.Copy(w, resp.Body)
}

// API Key Validation endpoint - validates API keys for OpenRouter and OpenAI
// Capabilities endpoint
func (api *StreamingAPI) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Get current tracing provider
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers":   []string{"bedrock", "openai", "anthropic"},
		"streaming":   true,
		"sse":         true,
		"agent_modes": []string{"simple", "workflow"},
		"tracing": map[string]interface{}{
			"enabled":  tracingProvider != "noop",
			"provider": tracingProvider,
		},
		"workspace": map[string]interface{}{
			"github_sync_enabled": workspace.IsGitSyncEnabled(),
		},
		"servers":    []string{},
		"local_mode": IsLocalMode(),
	})
}

// handleCdpCheck checks if a CDP port is reachable on localhost
func (api *StreamingAPI) handleCdpCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	portStr := r.URL.Query().Get("port")
	if portStr == "" {
		portStr = "9222"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"error":     "invalid port number",
		})
		return
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
		})
		return
	}
	conn.Close()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected": true,
	})
}

// Query endpoint - handles POST requests to start agent streaming
func (api *StreamingAPI) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Parse request body first
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorMsg := fmt.Sprintf("Invalid request body: %v", err)
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Handle alias: Map Message to Query if Query is empty
	if req.Query == "" && req.Message != "" {
		req.Query = req.Message
	}

	// Validate required fields
	if req.Query == "" {
		errorMsg := "Query is required"
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Record start time for duration calculation
	startTime := time.Now()
	log.Printf("[LATENCY_DEBUG] T+0ms | Request received | query_preview=%q", truncateForLog(req.Query, 80))

	// Generate query ID
	queryID := fmt.Sprintf("query_%d", time.Now().UnixNano())

	// Initialize Langfuse tracing - single trace for entire conversation
	// Read tracing provider from environment variable, default to "noop"
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}
	tracer := observability.GetTracer(tracingProvider)
	traceName := fmt.Sprintf("agent-conversation: %s", r.Header.Get("X-Session-ID"))
	if traceName == "agent-conversation: " {
		traceName = fmt.Sprintf("agent-conversation: %s", queryID)
	}
	traceID := tracer.StartTrace(traceName, map[string]interface{}{
		"method":     r.Method,
		"url":        r.URL.String(),
		"user_agent": r.Header.Get("User-Agent"),
		"session_id": r.Header.Get("X-Session-ID"),
		"query":      req.Query,
		"query_id":   queryID,
	})

	// NOTE: For workflow mode, LLM selection follows priority: temp override → step config → preset LLM
	// No orchestrator default fallback. req.Provider and req.ModelID are not used for workflow agents.
	// For non-workflow agents (simple/chat mode), req.Provider and req.ModelID come from the frontend request.
	// Environment variable fallbacks have been removed - frontend always sends these values.

	// Default maxTurns only when omitted (0). Negative values are preserved to mean "no turn cap".
	// Multi-agent chat and the workflow builder run uncapped by default.
	isWorkflowBuilderPhase := req.AgentMode == "workflow_phase" && req.PhaseID == workflowtypes.WorkflowStatusWorkflowBuilder
	if req.MaxTurns == 0 {
		if req.AgentMode == "simple" || isWorkflowBuilderPhase {
			req.MaxTurns = -1
			log.Printf("[AGENT] MaxTurns omitted for %s mode, running without a turn cap", req.AgentMode)
		} else {
			req.MaxTurns = orchestrator.GetDefaultMaxTurnsFromEnv()
			log.Printf("[AGENT] MaxTurns not provided or 0, defaulting to %d (from env or 500)", req.MaxTurns)
		}
	}

	// Use enabled_servers if provided, otherwise fall back to servers
	selectedServers := req.EnabledServers
	if len(selectedServers) == 0 {
		selectedServers = req.Servers
	}

	// Strip browser-specific MCP servers (playwright) when no browser is selected in chat mode.
	// Workflow modes get their browser config from workflow.json, not from the request.
	if (req.BrowserMode == "" || req.BrowserMode == "none") && req.AgentMode != "workflow_phase" && req.AgentMode != "workflow" {
		var filteredForBrowser []string
		for _, s := range selectedServers {
			if s != "playwright" {
				filteredForBrowser = append(filteredForBrowser, s)
			}
		}
		if len(filteredForBrowser) != len(selectedServers) {
			log.Printf("[SERVERS] Stripped browser-specific servers (playwright) — no browser mode active (was %d, now %d)", len(selectedServers), len(filteredForBrowser))
			selectedServers = filteredForBrowser
		}
	}

	// Default to NO_SERVERS if none specified (user didn't select any MCP servers)
	// This ensures the orchestrator and all sub-agents correctly get "no servers"
	// instead of an empty slice which would be treated as "all servers" downstream
	if len(selectedServers) == 0 {
		selectedServers = []string{mcpclient.NoServers}
	}

	var serverList string
	// Check for explicit "NO_SERVERS" request (pure LLM mode, no tools)
	if len(selectedServers) == 1 && selectedServers[0] == mcpclient.NoServers {
		// Keep NoServers constant as-is - this will be handled by integration code
		serverList = mcpclient.NoServers
	} else {
		// Convert server array to comma-separated string for agent compatibility
		serverList = strings.Join(selectedServers, ",")
	}

	// Extract sessionID from header/cookie or fallback to queryID
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = queryID // fallback: use queryID as sessionID if not provided
	}

	// Get current user ID for session isolation
	currentUserID := GetUserIDFromContext(r.Context())
	queryLogCtx := requestLogContext(r.Context(), req, sessionID)
	logfWithContext(queryLogCtx, "[USER_ID_DEBUGGING] HTTP handler: currentUserID=%q (from auth context)", currentUserID)

	// Chat sessions are in-memory only — tracked via activeSessions map
	// below. No persistent session metadata.

	// Clear the stopped guard now that the user is explicitly sending a new message.
	// This must happen AFTER session reactivation and BEFORE workshop creation,
	// so the workshop code path sees a clean slate.
	api.clearSessionStopped(sessionID)

	// Track active session for page refresh recovery (no observer needed)
	api.trackActiveSession(sessionID, req.AgentMode, req.Query, currentUserID, req.BotPlatform, req.TriggeredBy)

	// Per-user memory and chat folders. Both live under _users/<userID>/ so different users
	// don't share each other's persistent memory or chat output files. If a prior LLMGuidance
	// endpoint call already set a session override, that wins; otherwise default to the
	// per-user path and pre-create the folder on disk so the first write succeeds.
	perUserMemoryFolder := perUserMemoryFolderFor(currentUserID)
	perUserChatsFolder := perUserChatsFolderFor(currentUserID)
	api.activeSessionsMux.Lock()
	if sess, ok := api.activeSessions[sessionID]; ok {
		if sess.MemoryFolder == "" {
			sess.MemoryFolder = perUserMemoryFolder
		} else {
			perUserMemoryFolder = sess.MemoryFolder
		}
		if sess.ChatsFolder == "" {
			sess.ChatsFolder = perUserChatsFolder
		} else {
			perUserChatsFolder = sess.ChatsFolder
		}
	}
	api.activeSessionsMux.Unlock()
	for _, rel := range []string{perUserMemoryFolder, perUserChatsFolder} {
		if err := createWorkspaceFolder(context.Background(), rel); err != nil {
			logfWithContext(queryLogCtx, "[SESSION] Warning: could not pre-create per-user folder %s: %v", rel, err)
		}
	}

	enableBrowserAccess := req.EnableBrowserAccess != nil && *req.EnableBrowserAccess
	cdpPort := 0
	if req.CdpPort != nil {
		cdpPort = *req.CdpPort
	}
	logfWithContext(
		queryLogCtx,
		"[QUERY] session=%s enable_browser_access=%v browser_mode=%q cdp_port=%d enabled_servers=%v llm_guidance_len=%d query=%q",
		sessionID,
		enableBrowserAccess,
		getBrowserMode(req),
		cdpPort,
		req.EnabledServers,
		len(req.LLMGuidance),
		req.Query,
	)
	logfWithContext(queryLogCtx, "[LATENCY_DEBUG] T+%dms | Session setup complete | sessionID=%s", time.Since(startTime).Milliseconds(), sessionID)

	// Create fresh agent for this request
	// Use LLM configuration from request if provided, otherwise use request defaults
	var finalProvider string
	var finalModelID string
	var fallbacks []agent.FallbackModel

	if isGlobalLLMConfigLocked() {
		// Locked mode: use server env for API keys; allow provider/model only if in default_published_llms
		if req.LLMConfig != nil && req.LLMConfig.Primary.Provider != "" && req.LLMConfig.Primary.ModelID != "" {
			p, m := req.LLMConfig.Primary.Provider, req.LLMConfig.Primary.ModelID
			if isAllowedDefaultLLM(p, m) {
				finalProvider, finalModelID = p, m
			} else {
				finalProvider, finalModelID = getPrimaryProviderAndModelFromDefaults()
			}
		} else {
			finalProvider, finalModelID = getPrimaryProviderAndModelFromDefaults()
		}
		supported := getSupportedProviders()
		if len(supported) > 0 {
			allowed := make(map[string]bool)
			for _, p := range supported {
				allowed[p] = true
			}
			if !allowed[finalProvider] {
				finalProvider = supported[0]
				finalModelID = llm.GetDefaultModel(llm.Provider(finalProvider))
			}
		}
		fallbacks = nil
	} else if req.LLMConfig != nil {
		// Use LLM configuration from frontend (new unified structure)
		finalProvider = req.LLMConfig.Primary.Provider
		finalModelID = req.LLMConfig.Primary.ModelID

		// Fallback to request defaults if LLMConfig is partially empty
		if finalProvider == "" {
			finalProvider = req.Provider
		}
		if finalModelID == "" {
			finalModelID = req.ModelID
		}

		// Convert Fallbacks to agent.FallbackModel slice
		for _, fallback := range req.LLMConfig.Fallbacks {
			fallbacks = append(fallbacks, agent.FallbackModel{
				Provider: fallback.Provider,
				ModelID:  fallback.ModelID,
			})
		}
	} else {
		// Fall back to request defaults
		finalProvider = req.Provider
		finalModelID = req.ModelID
	}

	// Session config isn't persisted anymore — follow-up messages rely on the
	// frontend to pass the provider/model on every request.

	// Handle workflow phase chat mode - convert to simple agent with phase-specific prompt + tools
	// This runs BEFORE the workflow orchestrator branch to intercept and redirect
	isWorkflowPhase := req.AgentMode == "workflow_phase"
	workflowPhaseID := req.PhaseID
	workflowPhaseFolder := "" // The preset's SelectedFolder — used to auto-grant write access in FolderGuard
	_ = workflowPhaseFolder   // used later in the function
	if isWorkflowPhase {
		logfWithContext(queryLogCtx, "[WORKFLOW_PHASE] Phase chat mode detected: phase=%s preset=%s session=%s", workflowPhaseID, req.PresetQueryID, sessionID)
		if req.PresetQueryID == "" {
			logfWithContext(queryLogCtx, "[WORKFLOW_PHASE] ERROR: workflow_phase mode requires a preset_query_id")
			http.Error(w, `{"error":"workflow_phase mode requires a preset_query_id (workflow preset)"}`, http.StatusBadRequest)
			return
		}

		// Try manifest-first resolution for workflow_phase
		// Priority: resolve from preset DB → fallback to req.SelectedFolder (scheduler sets this directly)
		phaseManifestLoaded := false
		resolvedWPath := ""
		if wPath, wErr := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); wErr == nil && wPath != "" {
			resolvedWPath = wPath
		} else if req.SelectedFolder != "" {
			// Scheduler/cron sets selected_folder directly — no DB lookup needed
			resolvedWPath = req.SelectedFolder
			logfWithContext(queryLogCtx.WithWorkflow(resolvedWPath), "[WORKFLOW_PHASE] Using selected_folder as workspace path: %s", resolvedWPath)
		}
		if resolvedWPath != "" {
			api.activeSessionsMux.Lock()
			if sess, ok := api.activeSessions[sessionID]; ok {
				workflowName := workflowNameFromWorkspacePath(resolvedWPath)
				sess.PresetQueryID = req.PresetQueryID
				sess.WorkspacePath = resolvedWPath
				sess.WorkflowName = workflowName
				sess.WorkflowLabel = workflowName
				sess.PresetName = workflowName
				if workflowPhaseID != "" {
					sess.CurrentExecutionName = workflowPhaseID
				}
			}
			api.activeSessionsMux.Unlock()
		}
		if resolvedWPath != "" {
			manifest, found, mErr := ReadWorkflowManifest(context.Background(), resolvedWPath)
			if mErr == nil && found {
				phaseManifestLoaded = true
				workflowPhaseFolder = resolvedWPath
				logfWithContext(queryLogCtx.WithWorkflow(resolvedWPath), "[WORKFLOW_PHASE] Loaded config from manifest at %s", resolvedWPath)
				if manifest.Capabilities.LLMConfig != nil && manifest.Capabilities.LLMConfig.PhaseLLM != nil {
					finalProvider = manifest.Capabilities.LLMConfig.PhaseLLM.Provider
					finalModelID = manifest.Capabilities.LLMConfig.PhaseLLM.ModelID
					logfWithContext(queryLogCtx.WithWorkflow(resolvedWPath), "[WORKFLOW_PHASE] Using phase LLM from manifest: %s/%s", finalProvider, finalModelID)
				}
				// If manifest has explicit selection, use it; otherwise leave nil (= all globals included)
				if req.SelectedGlobalSecrets == nil && manifest.Capabilities.SelectedGlobalSecretNames != nil {
					req.SelectedGlobalSecrets = manifest.Capabilities.SelectedGlobalSecretNames
				}
				// Manifest is the source of truth for workflow-selected user secrets too.
				req.DecryptedSecrets = api.loadSelectedUserSecrets(context.Background(), currentUserID, manifest.Capabilities.SelectedSecrets)

				// Manifest is the source of truth for servers and browser mode.
				if len(manifest.Capabilities.SelectedServers) > 0 {
					selectedServers = manifest.Capabilities.SelectedServers
					serverList = strings.Join(selectedServers, ",")
				}
				if manifest.Capabilities.BrowserMode != "" {
					req.BrowserMode = manifest.Capabilities.BrowserMode
				}
			}
		}

		if !phaseManifestLoaded {
			// Manifest-only mode: workflow.json is the source of truth.
			logfWithContext(queryLogCtx, "[WORKFLOW_PHASE] WARNING: No workflow.json found for preset %s - phase will use request defaults only", req.PresetQueryID)
			// Still need to resolve workspace folder for FolderGuard write access
			if workflowPhaseFolder == "" {
				if wPath, wErr := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); wErr == nil && wPath != "" {
					workflowPhaseFolder = wPath
				} else if req.SelectedFolder != "" {
					workflowPhaseFolder = req.SelectedFolder
				}
			}
		}
		// Convert to simple agent mode so it falls through to the standard agent path
		req.AgentMode = "simple"
	}

	// Handle workflow mode - use workflow orchestrator
	if req.AgentMode == "workflow" {

		// Check if preset_id is provided and workflow is approved (in-memory runtime state)
		if req.PresetQueryID != "" {
			if wfState := getWorkflowRuntime(req.PresetQueryID); wfState != nil {
				log.Printf("[WORKFLOW CHECK] Found workflow runtime: workflowStatus=%s", wfState.WorkflowStatus)
				if wfState.WorkflowStatus == workflowtypes.WorkflowStatusPostVerification {
					log.Printf("[WORKFLOW CHECK] Workflow is approved - proceeding with execution")
				} else {
					log.Printf("[WORKFLOW CHECK] Workflow is not approved yet - proceeding with planning phase")
				}
			} else {
				log.Printf("[WORKFLOW CHECK] No workflow runtime state for preset_id %s - will proceed with defaults", req.PresetQueryID)
			}
		}

		// Create workflow event bridge for event emission
		workflowEventBridge := &eventbridge.WorkflowEventBridge{
			BaseEventBridge: &eventbridge.BaseEventBridge{
				EventStore: api.eventStore,
				SessionID:  sessionID,
				Logger:     api.logger,
				BridgeName: "workflow",
			},
		}

		// Create custom tools for workflow agents (workspace tools + human tools)
		// Workflow agents can be Simple or ReAct agents, tools are registered based on mode
		// TODO: Memory tools removed from workflow - only needed for individual React agents
		// memoryTools := virtualtools.CreateMemoryTools()
		// memoryExecutors := virtualtools.CreateMemoryToolExecutors()
		allTools, allExecutors, toolCategories := createCustomTools(true, currentUserID, sessionID) // Workflow mode: session-aware

		// NOTE: Workspace executor replacement with session + secrets happens after secrets are merged (see below).

		// Load selected tools, code execution mode, tool search mode, skills, and preset LLM config from preset if available (for workflow agents)
		var selectedTools []string
		var useCodeExecutionMode bool
		var selectedSkills []string
		var presetLLMConfig *workflowtypes.PresetLLMConfig

		// Try manifest-first resolution: resolve workspace path, then load from workflow.json
		// Priority: req.SelectedFolder (direct) > resolveWorkspacePathFromPreset (preset-based)
		manifestLoaded := false
		manifestWorkspacePath := ""
		if req.SelectedFolder != "" {
			manifestWorkspacePath = req.SelectedFolder
		} else if req.PresetQueryID != "" {
			if wPath, wErr := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); wErr == nil && wPath != "" {
				manifestWorkspacePath = wPath
			}
		}
		if manifestWorkspacePath != "" {
			caps, found, mErr := LoadManifestForExecution(context.Background(), manifestWorkspacePath)
			if mErr != nil {
				log.Printf("[MANIFEST] Error loading manifest from %s: %v (falling back to defaults)", manifestWorkspacePath, mErr)
			} else if found {
				manifestLoaded = true
				log.Printf("[MANIFEST] Loaded workflow config from manifest at %s", manifestWorkspacePath)
				selectedTools = caps.SelectedTools
				selectedSkills = caps.SelectedSkills
				presetLLMConfig = caps.LLMConfig

				if len(caps.SelectedServers) > 0 {
					selectedServers = caps.SelectedServers
					serverList = strings.Join(selectedServers, ",")
				}

				// Global secrets from manifest — if explicit selection, use it; otherwise leave nil (= all globals included)
				if caps.SelectedGlobalSecretNames != nil {
					req.SelectedGlobalSecrets = caps.SelectedGlobalSecretNames
				}
				// User-stored secrets from manifest are authoritative for workflow UI edits.
				req.DecryptedSecrets = api.loadSelectedUserSecrets(context.Background(), currentUserID, caps.SelectedSecrets)

				// Browser mode from manifest
				if caps.BrowserMode != "" && caps.BrowserMode != "none" && req.BrowserMode == "" {
					req.BrowserMode = caps.BrowserMode
				}
			}
		}

		if !manifestLoaded && req.PresetQueryID != "" {
			// Manifest-only mode: workflow.json is the source of truth for workflow config.
			// If no manifest was found, log a warning. The workflow will run with request defaults only.
			log.Printf("[MANIFEST] WARNING: No workflow.json found for preset %s - workflow will run with request defaults only. Run migration: POST /api/workflows/migrate", req.PresetQueryID)
		}

		// --- Post-load processing: browser, image gen, GWS skills ---
		// Runs after either manifest or preset loading has populated the config variables.

		// Resolve effective browser mode
		workflowBrowserMode := req.BrowserMode
		// Only register agent_browser for headless/CDP when no dedicated browser MCP server
		// (Playwright) is configured.
		hasPlaywrightServer := false
		for _, s := range selectedServers {
			if s == "playwright" {
				hasPlaywrightServer = true
			}
		}
		if hasPlaywrightServer {
			workflowBrowserMode = "playwright"
			log.Printf("[WORKFLOW] Playwright server detected — skipping agent_browser registration, using mode=%s", workflowBrowserMode)
		}
		// Store resolved browser mode on session for context-aware shell blocking
		if workflowBrowserMode != "" {
			common.SetSessionBrowserMode(sessionID, workflowBrowserMode)
		}
		if workflowBrowserMode == "headless" || workflowBrowserMode == "cdp" {
			wfCdpPort := getCdpPort(req)
			if wfCdpPort == 0 && workflowBrowserMode == "cdp" {
				wfCdpPort = 9222
			}

			browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()
			browserTools := virtualtools.CreateWorkspaceBrowserTools()
			browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, wfCdpPort)

			allTools = append(allTools, browserTools...)
			for name, executor := range browserExecutors {
				allExecutors[name] = executor
			}
			for _, tool := range browserTools {
				if tool.Function != nil {
					toolCategories[tool.Function.Name] = browserCategory
				}
			}
			log.Printf("[WORKFLOW] Added browser tools (mode=%s, cdp_port=%d, sessionID=%s)", workflowBrowserMode, wfCdpPort, sessionID)

			hasAgentBrowserSkill := false
			for _, skill := range selectedSkills {
				if skill == "agent-browser" {
					hasAgentBrowserSkill = true
					break
				}
			}
			if !hasAgentBrowserSkill {
				selectedSkills = append(selectedSkills, "agent-browser")
				log.Printf("[WORKFLOW] Auto-adding agent-browser skill for browser access")
			}

			var filteredServers []string
			for _, s := range selectedServers {
				if s != "playwright" {
					filteredServers = append(filteredServers, s)
				}
			}
			if len(filteredServers) != len(selectedServers) {
				log.Printf("[WORKFLOW] Headless browser mode: stripped playwright MCP server from server list (was %d, now %d)", len(selectedServers), len(filteredServers))
				selectedServers = filteredServers
				if len(selectedServers) == 0 {
					serverList = mcpclient.NoServers
				} else {
					serverList = strings.Join(selectedServers, ",")
				}
			}
		}

		// Load image generation from LLM config (works for both manifest and preset sources)
		if presetLLMConfig != nil && presetLLMConfig.EnableImageGeneration != nil && *presetLLMConfig.EnableImageGeneration {
			imgCfg := virtualtools.ImageGenExecutorConfig{
				WorkspaceAPIURL: getWorkspaceAPIURL(),
				UserID:          currentUserID,
			}
			if presetLLMConfig.ImageGenProvider != "" {
				imgCfg.Provider = presetLLMConfig.ImageGenProvider
			}
			if presetLLMConfig.ImageGenModelID != "" {
				imgCfg.ModelID = presetLLMConfig.ImageGenModelID
			}
			virtualtools.MergeImageToolExecutorsUntyped(imgCfg, allExecutors, toolCategories)
			log.Printf("[WORKFLOW] Updated image tool executors (provider=%s model=%s)", imgCfg.Provider, imgCfg.ModelID)
		}

		// Auto-add gws-* skills when GWS access is enabled (workflow mode)
		gwsWorkflowEnabled := req.EnableGWSAccess != nil && *req.EnableGWSAccess
		if !gwsWorkflowEnabled {
			for _, s := range req.EnabledServers {
				if s == "gws" {
					gwsWorkflowEnabled = true
					break
				}
			}
		}
		if gwsWorkflowEnabled {
			gwsSkills := []string{"gws-shared", "gws-drive", "gws-gmail", "gws-calendar", "gws-docs", "gws-sheets", "gws-slides"}
			existingSkills := make(map[string]bool)
			for _, sk := range selectedSkills {
				existingSkills[sk] = true
			}
			added := 0
			for _, gs := range gwsSkills {
				if !existingSkills[gs] {
					selectedSkills = append(selectedSkills, gs)
					added++
				}
			}
			if added > 0 {
				log.Printf("[GWS] Auto-added %d gws-* skills for workflow mode (enable_gws_access: true)", added)
			}
		}

		// Use selected tools from request if preset didn't provide any
		if len(selectedTools) == 0 && len(req.SelectedTools) > 0 {
			selectedTools = req.SelectedTools
			if len(selectedTools) > 0 {
				log.Printf("[TOOLS] Using %d specific tools from request", len(selectedTools))
			} else {
				log.Printf("[TOOLS] Request has empty tool selection - will use ALL tools from selected servers")
			}
		} else if len(selectedTools) == 0 {
			log.Printf("[TOOLS] No tool selection specified - will use ALL tools from selected servers")
		}

		// Workflow execution now always uses code execution mode. Browser access is
		// exposed as a tool/capability and must not disable the get_api_spec/API bridge.
		useCodeExecutionMode = true
		if workflowBrowserMode != "" && workflowBrowserMode != "none" {
			log.Printf("[CODE_EXECUTION] Code execution mode enabled with browser_mode=%s", workflowBrowserMode)
		} else {
			log.Printf("[CODE_EXECUTION] Code execution mode enabled")
		}

		// Inject merged API keys (env + workspace) into LLM config for workflow execution.
		// Without this, workflow agents (todo task orchestrators, sub-agents) won't have
		// provider API keys and CLI providers like gemini-cli will fail.
		workflowLLMConfig := req.LLMConfig
		if workflowLLMConfig == nil {
			workflowLLMConfig = &orchestrator.LLMConfig{}
		}
		workflowLLMConfig.APIKeys = MergedProviderAPIKeys(r.Context())

		// Create workflow orchestrator for this request.
		// Note: req.MaxTurns is already normalized earlier in the handler:
		// 0 => default, negative => uncapped, positive => explicit limit.
		// Note: provider and model parameters removed - LLM selection uses temp override → step config → preset LLM
		workflowOrchestrator, err := orchtypes.NewWorkflowOrchestrator(
			api.mcpConfigPath,    // mcpConfigPath
			api.temperature,      // temperature
			"workflow",           // agentMode
			api.logger,           // logger
			workflowEventBridge,  // eventBridge
			tracer,               // tracer
			selectedServers,      // selectedServers
			selectedTools,        // NEW: selectedTools
			useCodeExecutionMode, // NEW: code execution mode
			allTools,             // customTools
			allExecutors,         // customToolExecutors
			workflowLLMConfig,    // llmConfig (with merged API keys)
			req.MaxTurns,         // maxTurns (normalized earlier in the handler)
			toolCategories,       // NEW: toolCategories
			presetLLMConfig,      // preset LLM config for agent defaults
		)
		if err != nil {
			log.Printf("[WORKFLOW ERROR] Failed to create workflow orchestrator: %v", err)
			http.Error(w, fmt.Sprintf("Failed to create workflow orchestrator: %v", err), http.StatusInternalServerError)
			return
		}

		// Set selected skills on the orchestrator
		if len(selectedSkills) > 0 {
			workflowOrchestrator.SetSelectedSkills(selectedSkills)
			log.Printf("[SKILLS] Applied %d skills to workflow orchestrator: %v", len(selectedSkills), selectedSkills)
		}

		// Merge global secrets with user-supplied secrets, then set on orchestrator
		allSecrets := mergeGlobalSecrets(req.DecryptedSecrets, req.SelectedGlobalSecrets)
		if len(allSecrets) > 0 {
			entries := make([]orchestrator.SecretEntry, len(allSecrets))
			for i, s := range allSecrets {
				entries[i] = orchestrator.SecretEntry{Name: s.Name, Value: s.Value}
			}
			workflowOrchestrator.SetSecrets(entries)
			log.Printf("[SECRETS] Applied %d secrets (%d global + %d user) to workflow orchestrator", len(entries), len(entries)-len(req.DecryptedSecrets), len(req.DecryptedSecrets))
		}

		// Replace workspace advanced executors with session-aware versions that include secrets.
		// This must happen AFTER secrets are merged so secrets are available as shell env vars.
		// createCustomTools uses the no-session CreateWorkspaceAdvancedToolExecutors(),
		// which means MCP_SESSION_ID won't be set and secrets won't be in the shell env.
		secretEnvVars := make(map[string]string, len(allSecrets))
		for _, s := range allSecrets {
			secretEnvVars["SECRET_"+s.Name] = s.Value
		}
		sessionAwareExecutors, workspaceEnv := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(currentUserID, sessionID, secretEnvVars)
		for name, executor := range sessionAwareExecutors {
			allExecutors[name] = executor
		}
		log.Printf("[WORKFLOW] Replaced workspace executors with session-aware versions (userID=%q, sessionID=%q, secrets=%d)", currentUserID, sessionID, len(secretEnvVars))

		// Store workspace env map reference on orchestrator so that when the MCP session ID
		// changes (e.g., per-group in batch execution), MCP_API_URL in the env is updated
		// automatically. This prevents session registry misses that cause new browser instances.
		workflowOrchestrator.SetWorkspaceEnvRef(workspaceEnv)
		log.Printf("[WORKFLOW] Stored workspace env ref on orchestrator (MCP_API_URL=%s)", workspaceEnv["MCP_API_URL"])

		// Store workflow orchestrator for guidance injection
		api.storeWorkflowOrchestrator(sessionID, workflowOrchestrator)

		// Track HTTP session ID on the orchestrator so MCP sessions can be closed on stop
		workflowOrchestrator.SetHTTPSessionID(sessionID)

		if workflowBrowserMode != "" && workflowBrowserMode != "none" {
			workflowOrchestrator.SetBrowserMode(workflowBrowserMode)
			log.Printf("[WORKFLOW] Set browser mode on orchestrator: %s", workflowBrowserMode)
		}

		// Propagate CDP port for browser mode detection in execution agents
		// getCdpPort already checks req.BrowserMode == "cdp" and defaults to 9222
		if cdpPort := getCdpPort(req); cdpPort > 0 {
			workflowOrchestrator.SetCdpPort(cdpPort)
			log.Printf("[WORKFLOW] Set CDP port on orchestrator: %d (browser_mode=%s)", cdpPort, req.BrowserMode)
		}

		// Wire up live tool call query for workshop query_step_tools
		workflowOrchestrator.SetToolCallQueryFunc(formatToolCallSummaries(api))

		// Create a cancellable context for workflow execution using background context.
		// This prevents normal HTTP workflow requests from being canceled when the
		// request returns. Workflow runs launched internally from Multi Agent Chat use
		// a synthetic wfrun_* request whose context is owned by the background run
		// wrapper, so deriving from r.Context() lets stop_workflow_run/terminate_agent
		// cancel the actual orchestrator context instead of only the wrapper waiter.
		workflowBaseCtx := context.Background()
		if req.TriggeredBy == "chat_tool" && strings.HasPrefix(sessionID, "wfrun_") {
			workflowBaseCtx = r.Context()
		}
		workflowCtx, workflowCancel := context.WithCancel(workflowBaseCtx)

		// Inject user ID into the workflow context
		workflowCtx = context.WithValue(workflowCtx, common.UserIDKey, currentUserID)
		// Inject chat session ID so execute_shell_command can look up the session's
		// working directory and folder guard config from the global session map.
		// Without this, execution agents always get workspace root as their shell cwd.
		workflowCtx = context.WithValue(workflowCtx, common.ChatSessionIDKey, sessionID)

		// Store the cancel function for potential cancellation (keyed by queryID for independent executions)
		api.workflowOrchestratorContextMux.Lock()
		api.workflowOrchestratorContexts[queryID] = workflowCancel
		api.workflowOrchestratorContextMux.Unlock()

		// Track which queryIDs belong to this session (for handleStopSession)
		api.sessionQueryIDMux.Lock()
		api.sessionQueryIDs[sessionID] = append(api.sessionQueryIDs[sessionID], queryID)
		api.sessionQueryIDMux.Unlock()

		// Return immediate response with query ID
		response := QueryResponse{
			QueryID:   queryID,
			SessionID: sessionID, // Include the actual session ID used for conversation history
			Status:    "started",
			Message:   "Query processing started. Use polling API to get real-time updates.",
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
			return
		}

		// Execute workflow asynchronously
		go func() {
			defer func() {
				// Clean up the cancel function when done (keyed by queryID)
				api.workflowOrchestratorContextMux.Lock()
				delete(api.workflowOrchestratorContexts, queryID)
				api.workflowOrchestratorContextMux.Unlock()

				// Remove from active executions registry
				api.activeWorkflowExecutionsMux.Lock()
				delete(api.activeWorkflowExecutions, queryID)
				api.activeWorkflowExecutionsMux.Unlock()
				api.finalizeTrackedExecutionIfRunning(queryID, trackedExecutionStatusCanceled, "workflow execution ended before completion was recorded")

				// Remove queryID from session tracking
				api.sessionQueryIDMux.Lock()
				if queryIDs, exists := api.sessionQueryIDs[sessionID]; exists {
					// Filter out this queryID
					newQueryIDs := make([]string, 0, len(queryIDs)-1)
					for _, qid := range queryIDs {
						if qid != queryID {
							newQueryIDs = append(newQueryIDs, qid)
						}
					}
					if len(newQueryIDs) > 0 {
						api.sessionQueryIDs[sessionID] = newQueryIDs
					} else {
						delete(api.sessionQueryIDs, sessionID)
					}
				}
				api.sessionQueryIDMux.Unlock()
			}()

			if isWorkflowPhase && workflowPhaseFolder != "" && workflowPhaseFolder != "default_workspace" {
				triggeredBy := "workflow_phase"
				if workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder {
					triggeredBy = "workflow_builder"
				}

				runFolder := ""
				if req.ExecutionOptions != nil {
					runFolder = req.ExecutionOptions.SelectedRunFolder
				}
				api.registerRunningWorkflow(&ActiveWorkflowExecution{
					QueryID:       queryID,
					SessionID:     sessionID,
					PresetQueryID: req.PresetQueryID,
					WorkspacePath: workflowPhaseFolder,
					RunFolder:     runFolder,
					PhaseID:       workflowPhaseID,
					Status:        "running",
					UserID:        currentUserID,
					Query:         req.Query,
					TriggeredBy:   triggeredBy,
					StartedAt:     time.Now(),
				})
			}

			// Check in-memory runtime state for workflow approval status
			workflowStatus := workflowtypes.WorkflowStatusPreVerification // Default status
			var selectedOptions *workflowtypes.WorkflowSelectedOptions
			var stepID string
			if req.PresetQueryID != "" {
				if wfState := getWorkflowRuntime(req.PresetQueryID); wfState != nil {
					workflowStatus = wfState.WorkflowStatus
					selectedOptions = wfState.SelectedOptions
					log.Printf("[WORKFLOW CHECK] Runtime state: workflowStatus=%s", workflowStatus)
				} else {
					log.Printf("[WORKFLOW CHECK] No runtime state for preset_id %s", req.PresetQueryID)
				}

				// Retrieve step_id if it was stored for this preset
				api.workflowStepIDMux.RLock()
				if api.workflowStepIDs != nil {
					if storedStepID, exists := api.workflowStepIDs[req.PresetQueryID]; exists {
						stepID = storedStepID
						log.Printf("[WORKFLOW CHECK] Found step_id for preset: %s", stepID)
						// Clear it after retrieval (one-time use)
						delete(api.workflowStepIDs, req.PresetQueryID)
					}
				}
				api.workflowStepIDMux.RUnlock()
			} else {
				log.Printf("[WORKFLOW CHECK] No preset_query_id provided, using default workflowStatus: %s", workflowStatus)
			}

			// Chat-only phases should not go through the orchestrator path.
			// If the database has these as workflow status, reject early with a clear message.
			if workflowStatus == workflowtypes.WorkflowStatusWorkflowBuilder {
				log.Printf("[WORKFLOW ERROR] Phase %q is chat-only — cannot execute via orchestrator. Use phase chat mode instead.", workflowStatus)
				api.eventStore.AddEvent(sessionID, events.Event{
					ID:        fmt.Sprintf("chat_only_error_%d", time.Now().UnixNano()),
					Type:      "workflow_error",
					Timestamp: time.Now(),
					Data: &unifiedevents.AgentEvent{
						Type:      "workflow_error",
						Timestamp: time.Now(),
						Data: &unifiedevents.GenericEventData{
							Data: map[string]interface{}{
								"error": fmt.Sprintf("%s is a chat-only phase. Please use the phase chat tab instead of the Execute button.", workflowStatus),
							},
						},
					},
				})
				return
			}

			log.Printf("[WORKFLOW EXECUTION] Executing workflow with status: %s", workflowStatus)
			if stepID != "" {
				log.Printf("[WORKFLOW EXECUTION] Step-specific execution for step: %s", stepID)
			}

			// Get the actual objective and workspace path — try SelectedFolder first, then preset
			workflowObjective := req.Query // Default to query if not available
			workflowWorkspacePath := ""
			execManifestResolved := false

			// Resolve workspace path: direct > preset-based
			execWorkspacePath := ""
			if req.SelectedFolder != "" {
				execWorkspacePath = req.SelectedFolder
			} else if req.PresetQueryID != "" {
				if wPath, wErr := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); wErr == nil && wPath != "" {
					execWorkspacePath = wPath
				}
			}

			if execWorkspacePath != "" {
				_, found, mErr := ReadWorkflowManifest(context.Background(), execWorkspacePath)
				if mErr == nil && found {
					execManifestResolved = true
					workflowWorkspacePath = execWorkspacePath
					// Objective comes from variables/variables.json, not the manifest
					log.Printf("[WORKFLOW EXECUTION] Using manifest: workspace=%s", execWorkspacePath)
				}
			}
			if !execManifestResolved && execWorkspacePath != "" {
				workflowWorkspacePath = execWorkspacePath
				log.Printf("[MANIFEST] WARNING: No workflow.json found at %s - using request defaults", execWorkspacePath)
			}

			// Fallback: Extract workspace path from objective if not found in preset
			if workflowWorkspacePath == "" {
				workflowWorkspacePath = extractWorkspacePathFromObjective(workflowObjective)
				if workflowWorkspacePath == "" {
					log.Printf("[WORKFLOW ERROR] Workspace path not found in objective for query %s", queryID)
					workflowWorkspacePath = "default_workspace" // fallback
				}
			}

			// Register in active executions registry
			activeExec := &ActiveWorkflowExecution{
				QueryID:       queryID,
				SessionID:     sessionID,
				PresetQueryID: req.PresetQueryID,
				WorkspacePath: workflowWorkspacePath,
				RunFolder:     "iteration-0",
				Status:        "running",
				UserID:        currentUserID,
				Query:         req.Query,
				TriggeredBy:   "manual",
				StartedAt:     time.Now(),
			}
			if req.TriggeredBy != "" {
				activeExec.TriggeredBy = req.TriggeredBy
			}
			api.registerRunningWorkflow(activeExec)

			// Prepare options for the Execute method
			workflowOptions := map[string]interface{}{
				"workflowStatus":  workflowStatus,  // Current workflow status
				"selectedOptions": selectedOptions, // Pass selected options from database
			}
			if stepID != "" {
				workflowOptions["stepId"] = stepID // Pass step ID for step-specific phase execution
			}

			// Pass execution options from frontend if provided
			log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Received request - req.ExecutionOptions is nil: %v", req.ExecutionOptions == nil)
			if req.ExecutionOptions != nil {
				// Always run in iteration-0 — controller handles backup of previous iteration-0
				req.ExecutionOptions.SelectedRunFolder = "iteration-0"
				req.ExecutionOptions.RunMode = "use_same_run"

				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Execution options received: %+v", req.ExecutionOptions)
				log.Printf("[WORKFLOW EXECUTION] Frontend execution options provided: run_mode=%s, strategy=%s, run_folder=%s, resume_from_step=%d, enabled_group_names=%v, save_validation_responses=%v",
					req.ExecutionOptions.RunMode, req.ExecutionOptions.ExecutionStrategy, req.ExecutionOptions.SelectedRunFolder, req.ExecutionOptions.ResumeFromStep, req.ExecutionOptions.EnabledGroupNames, req.ExecutionOptions.SaveValidationResponses)

				// Convert to controller ExecutionOptions and pass to workflow orchestrator
				controllerOpts := &todo_creation_human.ExecutionOptions{
					RunMode:           req.ExecutionOptions.RunMode,
					SelectedRunFolder: req.ExecutionOptions.SelectedRunFolder,
					ExecutionStrategy: req.ExecutionOptions.ExecutionStrategy,
					ResumeFromStep:    req.ExecutionOptions.ResumeFromStep,
					PlanChangeAction:  req.ExecutionOptions.PlanChangeAction,
					EnabledGroupNames: req.ExecutionOptions.EnabledGroupNames,
				}

				// Set execution options on the workflow orchestrator
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Setting execution options on orchestrator: %+v", controllerOpts)
				workflowOrchestrator.SetExecutionOptions(controllerOpts)
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Execution options set on orchestrator successfully")
			} else {
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] No execution options provided in request - req.ExecutionOptions is nil")
			}

			// Set default working directory and folder guard for workflow shell commands
			if workflowWorkspacePath != "" {
				workspace.SetSessionWorkingDir(sessionID, workflowWorkspacePath)
				workspace.SetSessionFolderGuard(sessionID,
					[]string{workflowWorkspacePath},
					[]string{workflowWorkspacePath},
				)
			}

			// Update run_metadata.json with LLM config before execution starts
			if req.ExecutionOptions != nil && workflowWorkspacePath != "" {
				runFolder := req.ExecutionOptions.SelectedRunFolder
				if runFolder != "" {
					metaPath := workflowWorkspacePath + "/runs/" + runFolder + "/run_metadata.json"
					if existingMeta, err := readRunMetadata(workflowCtx, metaPath); err == nil && existingMeta != nil {
						models := &RunMetadataModels{}
						if presetLLMConfig != nil {
							models.AllocationMode = presetLLMConfig.LLMAllocationMode
							if presetLLMConfig.LearningLLM != nil {
								models.LearningLLM = &RunMetadataLLM{Provider: presetLLMConfig.LearningLLM.Provider, ModelID: presetLLMConfig.LearningLLM.ModelID}
							}
							if presetLLMConfig.PhaseLLM != nil {
								models.PhaseLLM = &RunMetadataLLM{Provider: presetLLMConfig.PhaseLLM.Provider, ModelID: presetLLMConfig.PhaseLLM.ModelID}
							}
							if presetLLMConfig.TieredConfig != nil {
								if presetLLMConfig.TieredConfig.Tier1 != nil {
									models.Tier1 = &RunMetadataLLM{Provider: presetLLMConfig.TieredConfig.Tier1.Provider, ModelID: presetLLMConfig.TieredConfig.Tier1.ModelID}
								}
								if presetLLMConfig.TieredConfig.Tier2 != nil {
									models.Tier2 = &RunMetadataLLM{Provider: presetLLMConfig.TieredConfig.Tier2.Provider, ModelID: presetLLMConfig.TieredConfig.Tier2.ModelID}
								}
								if presetLLMConfig.TieredConfig.Tier3 != nil {
									models.Tier3 = &RunMetadataLLM{Provider: presetLLMConfig.TieredConfig.Tier3.Provider, ModelID: presetLLMConfig.TieredConfig.Tier3.ModelID}
								}
							}
						}
						existingMeta.Models = models
						_ = writeRunMetadata(workflowCtx, metaPath, existingMeta)
					}
				}
			}

			// Execute workflow with the preset objective (not the phase query)
			log.Printf("[WORKFLOW DEBUG] Starting workflow execution for query %s with objective: %s, workspace: %s", queryID, workflowObjective, workflowWorkspacePath)
			_, err := workflowOrchestrator.Execute(
				workflowCtx,
				workflowObjective, // Use preset objective instead of req.Query
				workflowWorkspacePath,
				workflowOptions,
			)
			if err != nil {
				// Check if this is a zombie execution: if our queryID is no longer registered
				// for this session, the session was stopped/replaced by a newer execution.
				// Avoid overwriting the newer execution's session status with our stale error.
				api.sessionQueryIDMux.RLock()
				isCurrentExecution := false
				for _, qid := range api.sessionQueryIDs[sessionID] {
					if qid == queryID {
						isCurrentExecution = true
						break
					}
				}
				api.sessionQueryIDMux.RUnlock()

				if !isCurrentExecution {
					log.Printf("[WORKFLOW COMPLETION] Skipping error status update for zombie execution %s (session %s has a newer execution or was intentionally stopped)", queryID, sessionID)
					return
				}

				log.Printf("[WORKFLOW ERROR] Workflow execution failed for query %s: %v", queryID, err)

				// Extract root cause error from the error chain
				rootCauseError := extractRootCauseError(err)
				fullError := err.Error()

				// Emit UnifiedCompletionEvent with root cause error (for UI display)
				errorEventData := unifiedevents.NewUnifiedCompletionEventWithError(
					"workflow",            // agentType
					"workflow",            // agentMode
					workflowObjective,     // question
					rootCauseError,        // root cause error message
					time.Since(startTime), // duration
					0,                     // turns
				)
				agentEvent := unifiedevents.NewAgentEvent(errorEventData)
				agentEvent.SessionID = sessionID
				status := trackedExecutionStatusFailed
				if strings.Contains(fullError, "context canceled") || strings.Contains(fullError, "context deadline exceeded") {
					status = trackedExecutionStatusCanceled
				}
				api.completeTrackedExecution(queryID, status, rootCauseError, nil)
				completionEvent := events.Event{
					ID:        fmt.Sprintf("workflow_completion_error_%s_%d", queryID, time.Now().UnixNano()),
					Type:      string(unifiedevents.EventTypeUnifiedCompletion),
					Timestamp: time.Now(),
					Data:      agentEvent,
					SessionID: sessionID,
				}
				api.eventStore.AddEvent(sessionID, completionEvent)
				log.Printf("[WORKFLOW ERROR] Emitted UnifiedCompletionEvent with root cause error for query %s: %s", queryID, rootCauseError)

				// Also send workflow_error event with both root cause and full chain
				errorData := map[string]interface{}{
					"error":       rootCauseError, // Root cause (most important)
					"error_chain": fullError,      // Full error chain for debugging
					"query_id":    queryID,
				}
				api.eventStore.AddEvent(sessionID, events.Event{
					ID:        fmt.Sprintf("workflow_error_%s_%d", queryID, time.Now().UnixNano()),
					Type:      "workflow_error",
					Timestamp: time.Now(),
					Data: &unifiedevents.AgentEvent{
						Type:      "workflow_error",
						Timestamp: time.Now(),
						Data: &unifiedevents.GenericEventData{
							Data: errorData,
						},
					},
					SessionID: sessionID,
				})

				// Update active session status to error
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to error", sessionID)
				api.updateSessionStatus(sessionID, "error")
				// Clean up HTTP session → MCP session tracker on error completion
				mcpagent.CloseHTTPSession(sessionID)
				// Kill headless browser processes for this session
				api.cleanupBrowserSessions(sessionID)
			} else {
				log.Printf("[WORKFLOW DEBUG] Workflow execution completed for query %s", queryID)
				// Workflow completion events are now handled by the workflow orchestrator itself
				api.completeTrackedExecution(queryID, trackedExecutionStatusCompleted, "", nil)

				// Update active session status to completed
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to completed", sessionID)
				api.updateSessionStatus(sessionID, "completed")
				// Clean up HTTP session → MCP session tracker on successful completion
				mcpagent.CloseHTTPSession(sessionID)
				// Kill headless browser processes for this session
				api.cleanupBrowserSessions(sessionID)
			}
		}()
		return
	}

	// Load preset LLM config for chat/simple mode (for feature toggle fallbacks)
	// Source: workflow.json manifest (no DB dependency)
	var presetLLMConfig *workflowtypes.PresetLLMConfig
	{
		wsPath := req.SelectedFolder
		if wsPath == "" && req.PresetQueryID != "" {
			if p, e := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); e == nil && p != "" {
				wsPath = p
			}
		}
		if wsPath != "" {
			if manifest, found, mErr := ReadWorkflowManifest(context.Background(), wsPath); mErr == nil && found && manifest.Capabilities.LLMConfig != nil {
				presetLLMConfig = manifest.Capabilities.LLMConfig
			}
		}
	}

	// Return immediate response with query ID
	response := QueryResponse{
		QueryID:   queryID,
		SessionID: sessionID, // Include the actual session ID used for conversation history
		Status:    "started",
		Message:   "Query processing started. Use polling API to get real-time updates.",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}

	// Don't clear events - let the frontend handle event continuation
	// The deduplication logic in the frontend will handle any duplicates

	// Store last query request for synthetic turns and set session busy
	if !isWorkflowPhase {
		req.userID = currentUserID
		api.lastQueryMu.Lock()
		api.lastQueryRequests[sessionID] = req
		api.lastQueryMu.Unlock()

		// If a synthetic (auto-notification) turn is running, cancel it so user gets priority.
		// The synthetic turn's auto-notification content is already in conversation history,
		// so the agent will see it as context when processing the user's message.
		if api.isSyntheticTurn(sessionID) {
			api.agentCancelMux.RLock()
			cancelFn, hasCancelFn := api.agentCancelFuncs[sessionID]
			api.agentCancelMux.RUnlock()
			if hasCancelFn {
				log.Printf("[SYNTHETIC_TURN] Canceling synthetic turn for session %s — user message takes priority", sessionID)
				cancelFn()
				// Wait briefly for the synthetic turn goroutine to clean up
				time.Sleep(100 * time.Millisecond)
			}
		}

		api.setSessionBusy(sessionID, true)
		// Mark auto-notification turns as synthetic so frontend doesn't block input
		if req.IsAutoNotification {
			api.setSyntheticTurn(sessionID, true)
		} else {
			api.setSyntheticTurn(sessionID, false)
		}
	}

	// Load merged API keys (env + workspace) while r.Context() is still valid (before goroutine)
	mergedAPIKeys := MergedProviderAPIKeys(r.Context())

	// Process the query in the background
	go func() {
		// Clear session busy when the agent turn completes
		if !isWorkflowPhase {
			defer func() {
				api.setSyntheticTurn(sessionID, false)
				api.setSessionBusy(sessionID, false)
				// Drain pending completions after turn ends (batched to avoid concurrent StreamWithEvents)
				pending := api.drainPendingCompletions(sessionID)
				if len(pending) > 0 {
					go api.processBatchedBackgroundAgentCompletions(sessionID, pending)
				}
			}()
		}

		// Helper function to send error and continue (not terminate)
		sendError := func(errorMsg string, shouldTerminate bool) {
			if shouldTerminate {
				tracer.EndTrace(traceID, map[string]interface{}{
					"status": "failed",
				})

				// Emit server-level error completion event
				// Create an error completion event using UnifiedCompletionEvent
				errorEventData := unifiedevents.NewUnifiedCompletionEventWithError(
					"server",              // agentType
					req.AgentMode,         // agentMode
					req.Query,             // question
					errorMsg,              // error message
					time.Since(startTime), // duration
					0,                     // turns
				)

				agentEvent := unifiedevents.NewAgentEvent(errorEventData)
				agentEvent.SessionID = sessionID

				serverErrorEvent := events.Event{
					ID:        fmt.Sprintf("server_error_%s_%d", queryID, time.Now().UnixNano()),
					Type:      string(unifiedevents.EventTypeUnifiedCompletion),
					Timestamp: time.Now(),
					Data:      agentEvent,
					SessionID: sessionID,
				}
				api.eventStore.AddEvent(sessionID, serverErrorEvent)
				log.Printf("[SERVER DEBUG] Emitted server error completion event for query %s", queryID)
			}
		}

		// Resolve tier config early so provider validation can use it.
		// Without this, internal callers (scheduler, bots) that don't pass an explicit
		// provider would fail validation even though the tier config has one.
		if !isWorkflowPhase && finalProvider == "" {
			if earlyTierConfig := LoadAndResolveTierConfig(context.Background(), req.DelegationTierConfig); earlyTierConfig != nil {
				if earlyTierConfig.Main != nil && earlyTierConfig.Main.Provider != "" && earlyTierConfig.Main.ModelID != "" {
					finalProvider = earlyTierConfig.Main.Provider
					finalModelID = earlyTierConfig.Main.ModelID
				} else if earlyTierConfig.High != nil && earlyTierConfig.High.Provider != "" && earlyTierConfig.High.ModelID != "" {
					finalProvider = earlyTierConfig.High.Provider
					finalModelID = earlyTierConfig.High.ModelID
				}
			}
		}

		// Validate provider (use finalProvider which reflects LLMConfig.Primary.Provider or tier config)
		providerToValidate := finalProvider
		if providerToValidate == "" {
			providerToValidate = req.Provider
		}
		llmProvider, err := llm.ValidateProvider(providerToValidate)
		if err != nil {
			sendError(fmt.Sprintf("Invalid provider: %v", err), true)
			return
		}

		// Validate LLM provider - no need to initialize since agent wrapper handles it
		_ = llmProvider // Use provider variable to avoid unused variable error

		// Create context with timeout for the entire streaming operation
		streamCtx, cancel := context.WithTimeout(context.Background(), 60*3*time.Minute)
		defer cancel()

		// Load selected tools and code execution mode from preset if available (for simple/ReAct agents)
		var selectedTools []string
		var useCodeExecutionMode bool

		// Load selected tools from manifest (no DB dependency)
		{
			wsPath := req.SelectedFolder
			if wsPath == "" && req.PresetQueryID != "" {
				if p, e := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); e == nil && p != "" {
					wsPath = p
				}
			}
			if wsPath != "" {
				if manifest, found, mErr := ReadWorkflowManifest(context.Background(), wsPath); mErr == nil && found {
					selectedTools = manifest.Capabilities.SelectedTools
					if len(selectedTools) > 0 {
						log.Printf("[TOOLS] Loaded %d specific tools from manifest", len(selectedTools))
					}
				}
			}
		}

		// Use selected tools from request if preset didn't provide any
		if len(selectedTools) == 0 && len(req.SelectedTools) > 0 {
			selectedTools = req.SelectedTools
			if len(selectedTools) > 0 {
				log.Printf("[TOOLS] Using %d specific tools from request", len(selectedTools))
			} else {
				log.Printf("[TOOLS] Request has empty tool selection - will use ALL tools from selected servers")
			}
		} else if len(selectedTools) == 0 {
			log.Printf("[TOOLS] No tool selection specified - will use ALL tools from selected servers")
		}

		// Multi-agent chat / generic agent always runs in code-execution mode
		// regardless of provider. Tool-search and simple-agent paths have been
		// retired. Provider-specific CLI handling (CLI prompt template, native
		// context, api-bridge tool mapping) is decided separately via
		// common.IsCLIProvider further down the request lifecycle.
		useCodeExecutionMode = true
		if req.BrowserMode != "" && req.BrowserMode != "none" {
			log.Printf("[CODE_EXECUTION] Code execution mode enabled with browser_mode=%s", req.BrowserMode)
		} else {
			log.Printf("[CODE_EXECUTION] Code execution mode enabled (always on)")
		}

		// In plan delegation mode, orchestrator uses Main tier model (falls back to High if Main not set)
		if !isWorkflowPhase {
			tierConfig := LoadAndResolveTierConfig(streamCtx, req.DelegationTierConfig)
			if tierConfig != nil {
				if tierConfig.Main != nil && tierConfig.Main.Provider != "" && tierConfig.Main.ModelID != "" {
					finalProvider = tierConfig.Main.Provider
					finalModelID = tierConfig.Main.ModelID
					fallbacks = convertTierFallbacksToAgentFallbacks(tierConfig.Main.Fallbacks, tierConfig.Main.Provider)
					log.Printf("[DELEGATION] Orchestrator using main tier model: %s/%s", finalProvider, finalModelID)
				} else if tierConfig.High != nil && tierConfig.High.Provider != "" && tierConfig.High.ModelID != "" {
					finalProvider = tierConfig.High.Provider
					finalModelID = tierConfig.High.ModelID
					fallbacks = convertTierFallbacksToAgentFallbacks(tierConfig.High.Fallbacks, tierConfig.High.Provider)
					log.Printf("[DELEGATION] Orchestrator using high tier model (main not set): %s/%s", finalProvider, finalModelID)
				}
			}
		}

		// Create new agent with streamCtx instead of r.Context()
		log.Printf("[AGENT CONFIG DEBUG] Creating agent with ServerName: %s, UseCodeExecutionMode: %v", serverList, useCodeExecutionMode)
		agentConfig := agent.LLMAgentConfig{
			Name:               "chat-agent",
			ServerName:         serverList, // Use full server list, not just first one
			ConfigPath:         api.mcpConfigPath,
			Provider:           llm.Provider(finalProvider),
			ModelID:            finalModelID,
			Temperature:        req.Temperature,
			MaxTurns:           req.MaxTurns,
			ToolChoice:         "auto",
			StreamingChunkSize: 50,
			Timeout:            0, // No per-Invoke timeout; streamCtx (3h) provides the outer bound
			ToolTimeout: func() time.Duration {
				if envVal := os.Getenv("TOOL_EXECUTION_TIMEOUT"); envVal != "" {
					if timeout, err := time.ParseDuration(envVal); err == nil && timeout > 0 {
						return timeout
					}
				}
				return 5 * time.Minute
			}(),
			SelectedTools: selectedTools, // NEW: Pass selected tools

			// Smart routing disabled - always use all available tools
			EnableSmartRouting: false,

			// Detailed LLM configuration from frontend (unified fallback structure)
			Fallbacks: fallbacks,
			// Code execution mode: When enabled, only virtual tools are added to LLM
			// MCP tools are accessed via generated Go code using discover_code_files and write_code
			UseCodeExecutionMode: useCodeExecutionMode,
			APIKeys:              mergedAPIKeys,
			// Context summarization configuration
			// Priority: Request > Environment Variable > Default (matches orchestrator defaults)
			EnableContextSummarization: func() bool {
				// Priority: Request > Preset > Environment Variable > Default
				// If explicitly set in request, use that value
				if req.EnableContextSummarization != nil {
					return *req.EnableContextSummarization
				}
				// Check preset LLM config
				if presetLLMConfig != nil && presetLLMConfig.EnableContextSummarization != nil {
					return *presetLLMConfig.EnableContextSummarization
				}
				// Check environment variable - default to enabled (true), can be disabled via "false"
				if envVal := os.Getenv("ENABLE_CONTEXT_SUMMARIZATION"); envVal == "false" {
					return false
				}
				return true // Default to enabled (matches orchestrator)
			}(),
			SummarizeOnTokenThreshold: func() bool {
				// If explicitly set in request, use that value
				if req.SummarizeOnTokenThreshold != nil {
					return *req.SummarizeOnTokenThreshold
				}
				// Check environment variable - default to enabled (true), can be disabled via "false"
				if envVal := os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD"); envVal == "false" {
					return false
				}
				return true // Default to enabled (matches orchestrator)
			}(),
			TokenThresholdPercent: func() float64 {
				// Request takes highest priority
				if req.TokenThresholdPercent > 0 {
					return req.TokenThresholdPercent
				}
				// Check environment variable
				if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
					if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
						return threshold
					}
				}
				// Default to 80% (0.8) - matches orchestrator
				return 0.8
			}(),
			SummarizeOnFixedTokenThreshold: func() bool {
				// If explicitly set in request, use that value
				if req.SummarizeOnFixedTokenThreshold != nil {
					return *req.SummarizeOnFixedTokenThreshold
				}
				// Check environment variable - default to enabled (true), can be disabled via "false"
				if envVal := os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD"); envVal == "false" {
					return false
				}
				return true // Default to enabled (matches orchestrator)
			}(),
			FixedTokenThreshold: func() int {
				// Request takes highest priority
				if req.FixedTokenThreshold > 0 {
					return req.FixedTokenThreshold
				}
				// Check environment variable
				if envVal := os.Getenv("FIXED_TOKEN_THRESHOLD"); envVal != "" {
					if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
						return threshold
					}
				}
				return 200000 // Default to 200k tokens (matches orchestrator)
			}(),
			SummaryKeepLastMessages: func() int {
				if req.SummaryKeepLastMessages > 0 {
					return req.SummaryKeepLastMessages
				}
				// Check environment variable
				if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
					if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
						return keepLast
					}
				}
				return 4 // Default to 4 messages (matches orchestrator)
			}(),
			// Context editing configuration
			// Priority: Request > Environment Variable > Default
			EnableContextEditing: func() bool {
				// Priority: Request > Preset > Environment Variable > Default
				// If explicitly set in request, use that value
				if req.EnableContextEditing != nil {
					return *req.EnableContextEditing
				}
				// Check preset LLM config
				if presetLLMConfig != nil && presetLLMConfig.EnableContextEditing != nil {
					return *presetLLMConfig.EnableContextEditing
				}
				// Check environment variable
				if envVal := os.Getenv("ENABLE_CONTEXT_EDITING"); envVal == "true" {
					return true
				}
				// Default to disabled (false), can be enabled via ENABLE_CONTEXT_EDITING=true
				return os.Getenv("ENABLE_CONTEXT_EDITING") == "true"
			}(),
			ContextEditingThreshold: func() int {
				// Request takes highest priority
				if req.ContextEditingThreshold > 0 {
					return req.ContextEditingThreshold
				}
				// Check environment variable
				if envVal := os.Getenv("CONTEXT_EDITING_THRESHOLD"); envVal != "" {
					if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
						return threshold
					}
				}
				// Default to 0 (use default: 100)
				return 0
			}(),
			ContextEditingTurnThreshold: func() int {
				// Request takes highest priority
				if req.ContextEditingTurnThreshold > 0 {
					return req.ContextEditingTurnThreshold
				}
				// Check environment variable
				if envVal := os.Getenv("CONTEXT_EDITING_TURN_THRESHOLD"); envVal != "" {
					if turnThreshold, err := strconv.Atoi(envVal); err == nil && turnThreshold > 0 {
						return turnThreshold
					}
				}
				// Default to 0 (use default: 5)
				return 0
			}(),
			// Context offloading: large output threshold
			// Tool outputs larger than this threshold (in tokens) are offloaded to filesystem
			LargeOutputThreshold: func() int {
				// Check environment variable
				if envVal := os.Getenv("LARGE_OUTPUT_THRESHOLD"); envVal != "" {
					if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
						return threshold
					}
				}
				// Default to 0 (use library default: 10000 tokens)
				return 0
			}(),
			// Parallel tool execution: enabled by default, can be disabled via ENABLE_PARALLEL_TOOL_EXECUTION=false
			EnableParallelToolExecution: func() bool {
				if envVal := os.Getenv("ENABLE_PARALLEL_TOOL_EXECUTION"); envVal == "false" {
					return false
				}
				return true // Default to enabled
			}(),
			// MCP session ID for connection reuse (e.g., Playwright browser sharing)
			// Use the chat session ID so all agents in the same session share MCP connections
			SessionID: sessionID,
			// User ID for per-user OAuth token isolation
			// This ensures MCP servers with OAuth use user-specific token files
			UserID: currentUserID,
		}

		// Set agent mode based on request
		agentConfig.AgentMode = mcpagent.SimpleAgent
		log.Printf("[AGENT DEBUG] Creating agent with mode: %s, servers: %s", agentConfig.AgentMode, serverList)
		log.Printf("[SMART ROUTING DEBUG] Smart routing enabled - MaxTools: %d, MaxServers: %d (using defaults for temperature/tokens)",
			agentConfig.SmartRoutingMaxTools, agentConfig.SmartRoutingMaxServers)
		logfWithContext(queryLogCtx, "[LATENCY_DEBUG] T+%dms | Agent config built, creating agent wrapper | provider=%s model=%s", time.Since(startTime).Milliseconds(), finalProvider, finalModelID)
		// Create LLM agent wrapper with trace using streamCtx
		llmAgent, err := agent.NewLLMAgentWrapperWithTrace(streamCtx, agentConfig, tracer, traceID, api.logger)
		if err != nil {
			logfWithContext(queryLogCtx, "[AGENT DEBUG] Failed to create LLM agent wrapper: %v", err)
			sendError(fmt.Sprintf("Failed to create agent: %v", err), true)
			return
		}
		logfWithContext(queryLogCtx, "[LATENCY_DEBUG] T+%dms | Agent wrapper created", time.Since(startTime).Milliseconds())

		// Prime MCP server configs in the session registry for chat mode.
		// Workflow mode does this inside the orchestrator; chat mode must do it here
		// so that browser-scoped servers (playwright) can lazy-connect on first tool call.
		if api.mcpConfig != nil {
			registry := mcpclient.GetSessionRegistry()
			for _, sName := range selectedServers {
				if sName == mcpclient.NoServers {
					continue
				}
				serverCfg, cfgErr := api.mcpConfig.GetServer(sName)
				if cfgErr != nil {
					continue
				}
				registry.StoreServerConfig(sessionID, sName, serverCfg)

				// For playwright in workflow phases, bind to the same deterministic
				// browser session that the workflow orchestrator uses. This lets the
				// workshop chat share the browser (and login state) with workflow steps.
				if sName == "playwright" && isWorkflowPhase && workflowPhaseFolder != "" {
					browserSessionID := resolveWorkflowBrowserSessionID(workflowPhaseFolder, "default-group")
					registry.StoreServerConfig(browserSessionID, sName, serverCfg)
					registry.RegisterBrowserSessionOverride(sessionID, browserSessionID)
					log.Printf("[MCP SESSION] Bound chat session %s to shared browser session %s for playwright", sessionID, browserSessionID)
				}
			}
		}

		// Add workspace tools to chat agents (multi-agent chat mode)
		// Workflow mode handles workspace tools differently, so exclude it
		isChatMode := req.AgentMode == "simple" || req.AgentMode == ""

		// Resolve all conditional folder-guard grants once for this request.
		// See conditional_grants.go for the registry. The result is reused across
		// every folder guard and system prompt site below.
		resolvedGrants := resolveConditionalGrants(req)

		// When skill-creator is selected, ensure it's installed (auto-fetch from GitHub
		// if missing). This is the one piece of grant-specific logic that doesn't fit
		// the registry — it's an install-on-demand side effect unique to skill-creator.
		if resolvedGrants.HasGrant("skill-creator") {
			workspaceAPIURL := api.GetAPIURL()
			_, err := skills.GetSkill(workspaceAPIURL, "skill-creator")
			if err != nil {
				log.Printf("[SKILL CREATOR] skill-creator not found, attempting import from GitHub...")
				_, err := skills.ImportGitHubSkill(workspaceAPIURL, "https://github.com/anthropics/skills/tree/main/skills/skill-creator", "")
				if err != nil {
					log.Printf("[SKILL CREATOR] Warning: Failed to import skill-creator: %v", err)
				} else {
					log.Printf("[SKILL CREATOR] Successfully imported skill-creator")
				}
			}
		}

		var memoryBgDelegate virtualtools.BackgroundDelegateFunc
		var workspaceEnv map[string]string // hoisted so secrets can be injected after allChatSecrets is computed
		log.Printf("[CHAT_TOOLS_DEBUG] isChatMode=%v agentNonNil=%v enableImageGenPtr=%v", isChatMode, llmAgent.GetUnderlyingAgent() != nil, req.EnableImageGeneration)

		// Extract #workflow read-only folders early — needed both inside isChatMode block
		// (for folder guard setup) and in the workflow_phase block (for shell isolator).
		_, workflowReadOnlyFolders := collectSplitFolderGuardFolders(req.Query, req.WorkflowContextPaths)

		if isChatMode && llmAgent.GetUnderlyingAgent() != nil {
			// Handle browser access: when enabled, add agent-browser skill
			enableBrowserAccess := false
			if req.EnableBrowserAccess != nil && *req.EnableBrowserAccess {
				enableBrowserAccess = true
				// Auto-add agent-browser skill if not already selected
				hasAgentBrowserSkill := false
				for _, skill := range req.SelectedSkills {
					if skill == "agent-browser" {
						hasAgentBrowserSkill = true
						break
					}
				}
				if !hasAgentBrowserSkill {
					req.SelectedSkills = append(req.SelectedSkills, "agent-browser")
				}
				log.Printf("[BROWSER] Auto-adding agent-browser skill and tool (enable_browser_access: true)")
			}

			// Auto-add gws-* skills when GWS access is enabled (or gws in enabled_servers for preset compat)
			gwsAccessEnabled := req.EnableGWSAccess != nil && *req.EnableGWSAccess
			if !gwsAccessEnabled {
				for _, s := range req.EnabledServers {
					if s == "gws" {
						gwsAccessEnabled = true
						break
					}
				}
			}
			if gwsAccessEnabled {
				gwsSkills := []string{"gws-shared", "gws-drive", "gws-gmail", "gws-calendar", "gws-docs", "gws-sheets", "gws-slides"}
				existingSkills := make(map[string]bool)
				for _, skill := range req.SelectedSkills {
					existingSkills[skill] = true
				}
				added := 0
				for _, gs := range gwsSkills {
					if !existingSkills[gs] {
						req.SelectedSkills = append(req.SelectedSkills, gs)
						added++
					}
				}
				if added > 0 {
					log.Printf("[GWS] Auto-added %d gws-* skills (enable_gws_access: true)", added)
				}
			}

			// Create Chats/ folder if it doesn't exist
			if err := skills.CreateFolder("Chats"); err != nil {
				log.Printf("[WORKSPACE] Warning: Could not create Chats/ folder: %v", err)
			}

			// Create skills/ folder if it doesn't exist
			if err := skills.CreateFolder("skills"); err != nil {
				log.Printf("[WORKSPACE] Warning: Could not create skills/ folder: %v", err)
			} else {
				// Create skills/custom/ folder for Skill Builder
				if err := skills.CreateFolder("skills/custom"); err != nil {
					log.Printf("[WORKSPACE] Warning: Could not create skills/custom/ folder: %v", err)
				} else {
					log.Printf("[WORKSPACE] Ensured skills/ and skills/custom/ folders exist")
				}
			}

			// Create subagents/ folder if it doesn't exist
			if err := skills.CreateFolder("subagents"); err != nil {
				log.Printf("[WORKSPACE] Warning: Could not create subagents/ folder: %v", err)
			} else {
				if err := skills.CreateFolder("subagents/custom"); err != nil {
					log.Printf("[WORKSPACE] Warning: Could not create subagents/custom/ folder: %v", err)
				}
			}

			// Create memories/ folder if it doesn't exist
			if err := skills.CreateFolder("memories"); err != nil {
				log.Printf("[WORKSPACE] Warning: Could not create memories/ folder: %v", err)
			}

			// Chat mode: LLM-visible workspace tools (advanced + image + video + audio + music)
			// Basic tools (list/read/write/search) and git tools are not needed — shell is sufficient.
			// These tools are restricted to the current workspace/chat folder guard.
			workspaceTools := append(virtualtools.CreateWorkspaceAdvancedTools(), virtualtools.CreateWorkspaceImageTools()...)
			workspaceTools = append(workspaceTools, virtualtools.CreateWorkspaceVideoTools()...)
			workspaceTools = append(workspaceTools, virtualtools.CreateWorkspaceAudioTools()...)
			workspaceTools = append(workspaceTools, virtualtools.CreateWorkspaceMusicTools()...)
			var workspaceExecutors map[string]func(ctx context.Context, args map[string]interface{}) (string, error)
			workspaceExecutors, workspaceEnv = virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(currentUserID, sessionID)
			virtualtools.MergeImageToolExecutors(virtualtools.ImageGenExecutorConfig{
				WorkspaceAPIURL: getWorkspaceAPIURL(),
				UserID:          currentUserID,
			}, workspaceExecutors, nil)
			virtualtools.MergeVideoToolExecutors(virtualtools.VideoGenExecutorConfig{
				WorkspaceAPIURL: getWorkspaceAPIURL(),
				UserID:          currentUserID,
			}, workspaceExecutors, nil)
			virtualtools.MergeAudioToolExecutors(virtualtools.AudioGenExecutorConfig{
				WorkspaceAPIURL: getWorkspaceAPIURL(),
				UserID:          currentUserID,
			}, workspaceExecutors, nil)
			virtualtools.MergeMusicToolExecutors(virtualtools.AudioGenExecutorConfig{
				WorkspaceAPIURL: getWorkspaceAPIURL(),
				UserID:          currentUserID,
			}, workspaceExecutors, nil)
			logfWithContext(queryLogCtx, "[USER_ID_DEBUGGING] Main agent workspace executors: created with explicit userID=%q sessionID=%q", currentUserID, sessionID)
			// Inject LLM config fallback for read_image HTTP calls (e.g., from claude CLI subprocess)
			if underlying := llmAgent.GetUnderlyingAgent(); underlying != nil {
				virtualtools.SetReadImageFallbackLLMConfig(workspaceExecutors, underlying.GetLLMModelConfig())
			}
			_, _, toolCategories := createCustomTools(false, currentUserID, sessionID)

			// Merge @context file paths into additional folder-guard write access.
			// workflowReadOnlyFolders was computed above.
			fileContextWriteFolders := extractFileContextWriteFolders(req.Query)
			if len(fileContextWriteFolders) > 0 {
				log.Printf("[FILE CONTEXT] Extracted write folder-guard paths from @context: %v", fileContextWriteFolders)
			}
			if len(workflowReadOnlyFolders) > 0 {
				log.Printf("[FILE CONTEXT] Extracted read-only folder-guard paths from #workflow: %v", workflowReadOnlyFolders)
			}

			// Workflow phase: grant write access to the whole workflow folder (prefix match)
			// and block writes to planning/ via the separate blocked-write list. This is
			// "allow everything except planning/" expressed as one prefix + one exception,
			// which is immune to the drift class of bugs that came from enumerating
			// individual writable subfolders (reports/, db/, soul/ previously fell out of
			// sync). planning/ stays read-only because plan.json / step_config.json /
			// workflow_layout.json must go through typed plan-mod tools that serialize
			// full structs, not raw writes.
			var fileContextBlockedWriteFolders []string
			if isWorkflowPhase && workflowPhaseFolder != "" {
				fileContextWriteFolders = append(fileContextWriteFolders, workflowPhaseFolder+"/")
				blockedPlanning := workflowPhaseFolder + "/" + todo_creation_human.PlanningFolderName + "/"
				fileContextBlockedWriteFolders = append(fileContextBlockedWriteFolders, blockedPlanning)
				log.Printf("[WORKFLOW_PHASE FOLDER GUARD] Write access: %s/ (whole workflow) with blocked-write prefix: %s", workflowPhaseFolder, blockedPlanning)
			}

			// Apply folder guard to restrict writes based on mode
			// Multi-agent (plan) mode: primary write folder is Chats/
			// Chat mode: writes go to Chats/
			if !isWorkflowPhase {
				// Per-user memory and chat folders replace the legacy global "memories/" and "Chats/" write paths.
				perUserMemWrite := perUserMemoryFolder + "/"
				perUserChatsWrite := perUserChatsFolder + "/"
				perUserChatHistory := strings.TrimSuffix(perUserChatsFolder, "Chats") + "chat_history/"
				additionalFolders := append([]string{}, resolvedGrants.WriteFolders...)
				additionalFolders = append(additionalFolders, fileContextWriteFolders...)
				additionalFolders = append(additionalFolders, perUserMemWrite)
				additionalFolders = append(additionalFolders, perUserChatHistory)
				workspaceExecutors = wrapExecutorsWithPlanFolderGuard(workspaceExecutors, perUserChatsFolder, workflowReadOnlyFolders, additionalFolders...)
				workspace.SetSessionWorkingDir(sessionID, "")
				readPaths := append([]string{perUserChatsWrite, perUserChatHistory, "skills/", "subagents/", "Downloads/", "Workflow/", "config/", perUserMemWrite}, additionalFolders...)
				readPaths = append(readPaths, resolvedGrants.ReadOnlyExtra...)
				readPaths = append(readPaths, workflowReadOnlyFolders...)
				workspace.SetSessionFolderGuard(sessionID,
					readPaths,
					append([]string{perUserChatsWrite, "Downloads/", "config/", perUserMemWrite, perUserChatHistory}, additionalFolders...),
				)
				log.Printf("[MULTI-AGENT FOLDER GUARD] Applied per-user folder restriction (chats: %s, mem: %s, write: %v, read-only: %v, grants: %v)", perUserChatsWrite, perUserMemWrite, additionalFolders, workflowReadOnlyFolders, resolvedGrants.AppliedNames)
			} else {
				perUserMemWrite := perUserMemoryFolder + "/"
				perUserChatsWrite := perUserChatsFolder + "/"
				perUserChatHistory := strings.TrimSuffix(perUserChatsFolder, "Chats") + "chat_history/"
				extraFolders := append([]string{"config/"}, resolvedGrants.WriteFolders...)
				extraFolders = append(extraFolders, fileContextWriteFolders...)
				extraFolders = append(extraFolders, perUserMemWrite)
				extraFolders = append(extraFolders, perUserChatsWrite)
				extraFolders = append(extraFolders, perUserChatHistory)
				workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors, workflowReadOnlyFolders, fileContextBlockedWriteFolders, extraFolders...)
				workspace.SetSessionWorkingDir(sessionID, "")
				readPaths := append([]string{perUserChatsWrite, perUserChatHistory, "Downloads/", "skills/", "subagents/", "Workflow/", "config/", perUserMemWrite}, extraFolders...)
				readPaths = append(readPaths, workflowReadOnlyFolders...)
				workspace.SetSessionFolderGuard(sessionID,
					readPaths,
					append([]string{perUserChatsWrite, "Downloads/", "config/", perUserMemWrite, perUserChatHistory}, extraFolders...),
				)
				// Blocked-write paths flow through to the isolator's
				// FolderGuardConfig.BlockedWritePaths and are enforced at kernel-sandbox
				// level — source of truth for what the shell can actually write. Matches
				// the blocked-write list applied to the typed-tool wrapper above so both
				// surfaces deny the same prefixes. Reads remain permitted so agents can
				// still inspect plan.json and friends.
				if len(fileContextBlockedWriteFolders) > 0 {
					workspace.SetSessionFolderGuardBlockedWritePaths(sessionID, fileContextBlockedWriteFolders)
				}
				log.Printf("[CHAT MODE FOLDER GUARD] Applied per-user folder restriction (chats: %s, mem: %s, read-only: %v, blocked-write: %v)", perUserChatsWrite, perUserMemWrite, workflowReadOnlyFolders, fileContextBlockedWriteFolders)
			}

			// Apply skill folder guard if skills are selected (read-only access to selected skills only)
			if len(req.SelectedSkills) > 0 {
				log.Printf("[SKILL FOLDER GUARD] Applied skill folder restriction - only selected skills accessible: %v", req.SelectedSkills)
			}

			log.Printf("[WORKSPACE TOOLS] Registering %d workspace tools for chat mode", len(workspaceTools))

			underlyingAgent := llmAgent.GetUnderlyingAgent()
			for _, tool := range workspaceTools {
				if tool.Function == nil {
					log.Printf("[WORKSPACE TOOLS] Warning: Skipping tool with nil Function")
					continue
				}
				toolName := tool.Function.Name
				if executor, exists := workspaceExecutors[toolName]; exists {
					// Enhance tool description based on mode
					var enhancedDescription string
					if !isWorkflowPhase {
						enhancedDescription = enhanceToolDescriptionForMultiAgentMode(toolName, tool.Function.Description, perUserChatsFolder)
					} else {
						enhancedDescription = enhanceToolDescriptionForChatMode(toolName, tool.Function.Description, perUserChatsFolder)
					}

					// Convert Parameters to map[string]interface{}
					var params map[string]interface{}
					if tool.Function.Parameters != nil {
						paramsBytes, err := json.Marshal(tool.Function.Parameters)
						if err == nil {
							json.Unmarshal(paramsBytes, &params)
						}
					}
					if params == nil {
						log.Printf("[WORKSPACE TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
						continue
					}

					// Get tool category from the category map - REQUIRED
					toolCategory := toolCategories[toolName]
					if toolCategory == "" {
						log.Printf("[WORKSPACE TOOLS ERROR] Tool %s not found in toolCategories map - category is REQUIRED!", toolName)
						sendError(fmt.Sprintf("Failed to register workspace tool %s: category is REQUIRED", toolName), true)
						return
					}

					// Executor is already the correct type (func(ctx, args) (string, error))
					// No type assertion needed unlike workflow where executors are map[string]interface{}
					if virtualtools.IsImageTool(toolName) && req.ImageGenConfig != nil {
						executor = virtualtools.WrapImageToolExecutorWithRuntimeOverride(executor, virtualtools.ImageGenRuntimeOverride{
							Provider: req.ImageGenConfig.Provider,
							ModelID:  req.ImageGenConfig.ModelID,
							APIKey:   req.ImageGenConfig.APIKey,
						})
					}

					if err := underlyingAgent.RegisterCustomTool(
						toolName,
						enhancedDescription,
						params,
						executor,
						toolCategory,
					); err != nil {
						log.Printf("[WORKSPACE TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
						sendError(fmt.Sprintf("Failed to register workspace tool %s: %v", toolName, err), true)
						return
					}
					log.Printf("[WORKSPACE TOOLS] Registered workspace tool: %s (category: %s)", toolName, toolCategory)
				}
			}
			log.Printf("[WORKSPACE TOOLS] Successfully registered %d workspace tools for chat mode", len(workspaceTools))

			// Register browser tool if browser access is enabled
			if enableBrowserAccess {
				browserTools := virtualtools.CreateWorkspaceBrowserTools()
				browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, getCdpPort(req))
				browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()

				// Apply same folder guard as workspace tools (reuse fileContextWriteFolders/workflowReadOnlyFolders from above)
				if !isWorkflowPhase {
					additionalFolders := append([]string{}, resolvedGrants.WriteFolders...)
					additionalFolders = append(additionalFolders, fileContextWriteFolders...)
					browserExecutors = wrapExecutorsWithPlanFolderGuard(browserExecutors, perUserChatsFolder, workflowReadOnlyFolders, additionalFolders...)
				} else {
					browserExtraFolders := append([]string{}, resolvedGrants.WriteFolders...)
					browserExtraFolders = append(browserExtraFolders, fileContextWriteFolders...)
					browserExtraFolders = append(browserExtraFolders, perUserChatsFolder+"/")
					browserExecutors = wrapExecutorsWithChatModeFolderGuard(browserExecutors, workflowReadOnlyFolders, fileContextBlockedWriteFolders, browserExtraFolders...)
				}
				log.Printf("[BROWSER TOOLS] Applied folder guard to browser tools")

				for _, tool := range browserTools {
					if tool.Function == nil {
						continue
					}
					toolName := tool.Function.Name
					if executor, exists := browserExecutors[toolName]; exists {
						var params map[string]interface{}
						if tool.Function.Parameters != nil {
							paramsBytes, err := json.Marshal(tool.Function.Parameters)
							if err == nil {
								json.Unmarshal(paramsBytes, &params)
							}
						}
						if params == nil {
							log.Printf("[BROWSER TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
							continue
						}

						if err := underlyingAgent.RegisterCustomTool(
							toolName,
							tool.Function.Description,
							params,
							executor,
							browserCategory,
						); err != nil {
							log.Printf("[BROWSER TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
							continue
						}
						log.Printf("[BROWSER TOOLS] Registered browser tool: %s (category: %s)", toolName, browserCategory)
					}
				}
				log.Printf("[BROWSER TOOLS] Successfully registered %d browser tools for chat mode", len(browserTools))
			}

			// Register delegation tool for multi-agent chat (all non-workflow-phase simple sessions).
			if !isWorkflowPhase {
				// Build delegation tier config early so we can pass it to tool creation (for dynamic enum)
				tierConfig := resolveDelegationTierConfig(req.DelegationTierConfig)
				delegationTools := virtualtools.CreateDelegationTools(tierConfig, true)
				delegationExecutors := virtualtools.CreateDelegationToolExecutors()
				delegationCategory := virtualtools.GetDelegationToolCategory()

				// Get underlying agent for tool registration
				delegationAgent := llmAgent.GetUnderlyingAgent()
				if delegationAgent == nil {
					logfWithContext(queryLogCtx, "[DELEGATION TOOLS ERROR] Cannot register delegation tools - underlying agent is nil")
				} else {
					// Create the delegation execution function that will spawn sub-agents
					// This function is injected into the context for the delegate tool to use
					executeDelegatedTask := func(subCtx context.Context, instruction string) (string, error) {
						return api.executeDelegatedTask(subCtx, req, sessionID, instruction)
					}

					// Create workspace client for plan file I/O. Scoped to the per-user Chats folder.
					planWorkspaceClient := workspace.NewClient(
						getWorkspaceAPIURL(),
						workspace.WithFolderGuard(&workspace.FolderGuardConfig{
							Enabled:      true,
							WritePaths:   []string{perUserChatsFolder},
							BlockedPaths: []string{},
						}),
						workspace.WithUserID(currentUserID),
					)

					// Build capabilities context for the delegation tools
					caps := buildCapabilitiesContext(req)

					// Create background delegate function for async delegation (all modes)
					bgDelegateFunc := func(bgCtx context.Context, name, instruction string) (string, error) {
						return api.executeBackgroundDelegatedTask(bgCtx, req, sessionID, name, instruction)
					}
					memoryBgDelegate = bgDelegateFunc
					bgQuerier := &bgAgentQuerierImpl{registry: api.bgAgentRegistry}

					// Register all delegation tools (agent decides autonomously what to use)
					for _, tool := range delegationTools {
						if tool.Function == nil {
							continue
						}
						toolName := tool.Function.Name

						if executor, exists := delegationExecutors[toolName]; exists {
							var params map[string]interface{}
							if tool.Function.Parameters != nil {
								paramsBytes, err := json.Marshal(tool.Function.Parameters)
								if err == nil {
									json.Unmarshal(paramsBytes, &params)
								}
							}
							if params == nil {
								logfWithContext(queryLogCtx, "[DELEGATION TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
								continue
							}

							// Capture executor for closure
							exec := executor

							// Wrap the executor to inject delegation function, workspace client, tier config, and capabilities
							wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
								ctx = context.WithValue(ctx, virtualtools.ExecuteDelegatedTaskKey, virtualtools.ExecuteDelegatedTaskFunc(executeDelegatedTask))
								ctx = context.WithValue(ctx, virtualtools.WorkspaceClientKey, planWorkspaceClient)
								ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, &sessionEventEmitter{
									eventStore: api.eventStore,
									sessionID:  sessionID,
								})
								// Propagate per-user memory + chats folders so sub-agents inherit them.
								ctx = context.WithValue(ctx, virtualtools.MemoryFolderKey, perUserMemoryFolder)
								ctx = context.WithValue(ctx, virtualtools.ChatsFolderKey, perUserChatsFolder)
								if tierConfig != nil {
									ctx = context.WithValue(ctx, virtualtools.DelegationTierConfigKey, tierConfig)
								}
								if caps != nil {
									ctx = context.WithValue(ctx, virtualtools.CapabilitiesContextKey, caps)
								}
								// Inject background delegation and agent querier for plan mode
								if bgDelegateFunc != nil {
									ctx = context.WithValue(ctx, virtualtools.BackgroundDelegateKey, bgDelegateFunc)
								}
								if bgQuerier != nil {
									ctx = context.WithValue(ctx, virtualtools.BGAgentRegistryKey, bgQuerier)
									ctx = context.WithValue(ctx, virtualtools.BGAgentSessionIDKey, sessionID)
								}
								return exec(ctx, args)
							}

							if err := delegationAgent.RegisterCustomToolWithTimeout(
								toolName,
								tool.Function.Description,
								params,
								wrappedExecutor,
								0, // No timeout — delegation tools run indefinitely (controlled by parent context)
								delegationCategory,
							); err != nil {
								logfWithContext(queryLogCtx, "[DELEGATION TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
								continue
							}
							logfWithContext(queryLogCtx, "[DELEGATION TOOLS] Registered delegation tool: %s (category: %s)", toolName, delegationCategory)
						}
					}
					logfWithContext(queryLogCtx, "[DELEGATION TOOLS] Successfully registered %d delegation tools for chat mode", len(delegationTools))

					// Register workflow run tools (run_workflow, run_step, stop_workflow_run)
					wfRunTools := createWorkflowRunTools()
					wfRunExecutors := createWorkflowRunExecutors(api)
					for _, tool := range wfRunTools {
						if tool.Function == nil {
							continue
						}
						toolName := tool.Function.Name
						if exec, exists := wfRunExecutors[toolName]; exists {
							var params map[string]interface{}
							if tool.Function.Parameters != nil {
								paramsBytes, _ := json.Marshal(tool.Function.Parameters)
								json.Unmarshal(paramsBytes, &params)
							}
							// Wrap to inject session context
							capturedExec := exec
							wrappedExec := func(ctx context.Context, args map[string]interface{}) (string, error) {
								ctx = context.WithValue(ctx, virtualtools.BGAgentSessionIDKey, sessionID)
								return capturedExec(ctx, args)
							}
							if err := delegationAgent.RegisterCustomToolWithTimeout(
								toolName,
								tool.Function.Description,
								params,
								wrappedExec,
								0,
								delegationCategory,
							); err != nil {
								logfWithContext(queryLogCtx, "[WORKFLOW_RUN_TOOLS] Failed to register %s: %v", toolName, err)
							} else {
								logfWithContext(queryLogCtx, "[WORKFLOW_RUN_TOOLS] Registered %s", toolName)
							}
						}
					}

					// Register workflow schedule tools (list/create/update/delete/trigger/get-runs)
					schedTools := createWorkflowScheduleTools()
					schedExecutors := createWorkflowScheduleExecutors(api, currentUserID)
					for _, tool := range schedTools {
						if tool.Function == nil {
							continue
						}
						toolName := tool.Function.Name
						exec, ok := schedExecutors[toolName]
						if !ok {
							continue
						}
						var params map[string]interface{}
						if tool.Function.Parameters != nil {
							paramsBytes, _ := json.Marshal(tool.Function.Parameters)
							json.Unmarshal(paramsBytes, &params)
						}
						capturedExec := exec
						wrappedExec := func(ctx context.Context, args map[string]interface{}) (string, error) {
							ctx = context.WithValue(ctx, virtualtools.BGAgentSessionIDKey, sessionID)
							return capturedExec(ctx, args)
						}
						if err := delegationAgent.RegisterCustomToolWithTimeout(
							toolName,
							tool.Function.Description,
							params,
							wrappedExec,
							0,
							delegationCategory,
						); err != nil {
							logfWithContext(queryLogCtx, "[WORKFLOW_SCHEDULE_TOOLS] Failed to register %s: %v", toolName, err)
						} else {
							logfWithContext(queryLogCtx, "[WORKFLOW_SCHEDULE_TOOLS] Registered %s", toolName)
						}
					}
				}
			}
		}

		// Add custom agent instructions based on agent mode
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			// Create custom tools for chat mode (workspace_advanced + workspace_image)
			allTools, allExecutors, toolCategories := createCustomTools(false, currentUserID, sessionID) // Chat mode: session-aware

			// In plan delegation mode (multi-agent), also include human tools (human_feedback)
			// Register each custom tool with the agent
			// This will trigger code generation and update the registry
			// Note: Workspace tools are already registered above, skip them in allTools
			registeredCount := 0
			for _, tool := range allTools {
				if tool.Function != nil {
					toolName := tool.Function.Name

					// Skip workspace tools - already registered above.
					switch toolCategories[toolName] {
					case "workspace_tools", virtualtools.GetWorkspaceAdvancedToolCategory(), virtualtools.GetWorkspaceBrowserToolCategory():
						continue
					}

					if executor, exists := allExecutors[toolName]; exists {
						// Convert executor to the expected function signature
						if execFunc, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
							// Convert Parameters to map[string]interface{} by marshaling/unmarshaling
							var params map[string]interface{}
							if tool.Function.Parameters != nil {
								paramsBytes, err := json.Marshal(tool.Function.Parameters)
								if err == nil {
									json.Unmarshal(paramsBytes, &params)
								}
							}
							if params == nil {
								params = make(map[string]interface{})
							}

							// Get tool category from the category map - REQUIRED
							toolCategory := toolCategories[toolName]
							if toolCategory == "" {
								log.Printf("[CUSTOM TOOLS ERROR] Tool %s not found in toolCategories map - category is REQUIRED!", toolName)
								// Continue to next tool instead of failing entire request
								continue
							}

							// Wrap human tools to inject SessionEventEmitter for blocking events (feedback/questions)
							registrationFunc := execFunc
							if toolCategory == virtualtools.GetHumanToolCategory() {
								originalExec := execFunc
								registrationFunc = func(ctx context.Context, args map[string]interface{}) (string, error) {
									ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, &sessionEventEmitter{
										eventStore: api.eventStore,
										sessionID:  sessionID,
									})
									return originalExec(ctx, args)
								}
							}

							// Register the tool - this triggers code generation
							if err := underlyingAgent.RegisterCustomTool(
								toolName,
								tool.Function.Description,
								params,
								registrationFunc,
								toolCategory,
							); err != nil {
								log.Printf("[CUSTOM TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
								// Continue to next tool instead of failing entire request
								continue
							}
							registeredCount++
							log.Printf("[CUSTOM TOOLS] Registered custom tool: %s (category: %s)", toolName, toolCategory)
						}
					}
				}
			}
			log.Printf("[CUSTOM TOOLS] Registered %d custom tools with agent", registeredCount)

			// Register memory tools in all chat modes.
			// In plan mode, memory tools can spawn background memory agents.
			// The memory workspace client is scoped to the per-user memory folder so all
			// memory file I/O lands under _users/<userID>/memories/.
			memoryTools := virtualtools.CreateMemoryTools()
			memoryExecutors := virtualtools.CreateMemoryToolExecutors()
			memoryCategory := virtualtools.GetDelegationToolCategory()
			memoryWritePaths := []string{perUserMemoryFolder, perUserChatsFolder}
			memoryWorkspaceClient := workspace.NewClient(
				getWorkspaceAPIURL(),
				workspace.WithFolderGuard(&workspace.FolderGuardConfig{
					Enabled:      true,
					WritePaths:   memoryWritePaths,
					BlockedPaths: []string{},
				}),
				workspace.WithUserID(currentUserID),
			)

			registeredMemoryCount := 0
			for _, tool := range memoryTools {
				if tool.Function == nil {
					continue
				}
				toolName := tool.Function.Name
				exec, exists := memoryExecutors[toolName]
				if !exists {
					continue
				}

				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					params = make(map[string]interface{})
				}

				execCopy := exec
				wrappedMemoryExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
					ctx = context.WithValue(ctx, virtualtools.WorkspaceClientKey, memoryWorkspaceClient)
					if memoryBgDelegate != nil {
						ctx = context.WithValue(ctx, virtualtools.BackgroundDelegateKey, memoryBgDelegate)
					}
					// Inject per-session memory and chats folder overrides
					api.activeSessionsMux.RLock()
					sess, sessExists := api.activeSessions[sessionID]
					api.activeSessionsMux.RUnlock()
					if sessExists && sess.MemoryFolder != "" {
						ctx = context.WithValue(ctx, virtualtools.MemoryFolderKey, sess.MemoryFolder)
					}
					if sessExists && sess.ChatsFolder != "" {
						ctx = context.WithValue(ctx, virtualtools.ChatsFolderKey, sess.ChatsFolder)
					}
					return execCopy(ctx, args)
				}

				if err := underlyingAgent.RegisterCustomToolWithTimeout(
					toolName,
					tool.Function.Description,
					params,
					wrappedMemoryExecutor,
					0, // Memory operations may involve background delegation; do not enforce timeout.
					memoryCategory,
				); err != nil {
					log.Printf("[MEMORY TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
					continue
				}
				registeredMemoryCount++
				log.Printf("[MEMORY TOOLS] Registered memory tool: %s (category: %s)", toolName, memoryCategory)
			}
			log.Printf("[MEMORY TOOLS] Registered %d memory tools with agent", registeredMemoryCount)

			isMultiAgentChat := !isWorkflowPhase
			if isMultiAgentChat {
				if err := api.registerMultiAgentLLMTools(underlyingAgent); err != nil {
					logfWithContext(queryLogCtx, "[LLM TOOLS] Failed to register multi-agent LLM tools: %v", err)
					sendError(fmt.Sprintf("Failed to register multi-agent LLM tools: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[LLM TOOLS] Registered multi-agent LLM tools")

				if err := api.registerMultiAgentSkillTools(underlyingAgent); err != nil {
					logfWithContext(queryLogCtx, "[SKILL TOOLS] Failed to register multi-agent skill tools: %v", err)
					sendError(fmt.Sprintf("Failed to register multi-agent skill tools: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[SKILL TOOLS] Registered multi-agent skill tools")
			}
			if isWorkflowPhase {
				if err := api.registerWorkflowLLMDiscoveryTools(underlyingAgent); err != nil {
					logfWithContext(queryLogCtx, "[LLM TOOLS] Failed to register workflow LLM discovery tools: %v", err)
					sendError(fmt.Sprintf("Failed to register workflow LLM discovery tools: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[LLM TOOLS] Registered workflow LLM discovery tools")
			}
			if isMultiAgentChat {
				if err := api.registerMultiAgentMCPServerTools(underlyingAgent); err != nil {
					logfWithContext(queryLogCtx, "[MCP SERVER TOOLS] Failed to register multi-agent MCP server tools: %v", err)
					sendError(fmt.Sprintf("Failed to register multi-agent MCP server tools: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[MCP SERVER TOOLS] Registered multi-agent MCP server tools")

				// create_workflow — privileged tool for writing new workflows under Workflow/
				// Bypasses the session folder guard by writing via direct filesystem I/O.
				if err := api.registerWorkflowCreatorTool(underlyingAgent); err != nil {
					logfWithContext(queryLogCtx, "[WORKFLOW CREATOR] Failed to register create_workflow tool: %v", err)
					sendError(fmt.Sprintf("Failed to register create_workflow tool: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[WORKFLOW CREATOR] Registered create_workflow tool")

				if err := api.registerActivityStatusTool(underlyingAgent, currentUserID); err != nil {
					logfWithContext(queryLogCtx, "[ACTIVITY STATUS] Failed to register get_activity_status tool: %v", err)
					sendError(fmt.Sprintf("Failed to register get_activity_status tool: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[ACTIVITY STATUS] Registered get_activity_status tool")

				if err := api.registerEmployeeManagementTools(underlyingAgent); err != nil {
					logfWithContext(queryLogCtx, "[EMPLOYEE TOOLS] Failed to register employee management tools: %v", err)
					sendError(fmt.Sprintf("Failed to register employee management tools: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[EMPLOYEE TOOLS] Registered employee management tools")

				if err := api.registerSecretManagementTools(underlyingAgent, currentUserID, "secret_tools", nil); err != nil {
					logfWithContext(queryLogCtx, "[SECRET TOOLS] Failed to register multi-agent secret tools: %v", err)
					sendError(fmt.Sprintf("Failed to register multi-agent secret tools: %v", err), true)
					return
				}
				logfWithContext(queryLogCtx, "[SECRET TOOLS] Registered multi-agent secret tools (list_secrets, set_user_secret, delete_user_secret)")
			}

			// Read session state early for guidance injection
			// (before delegation / memory instructions).
			// NOTE: UpdateCodeExecutionRegistry is called AFTER all AppendSystemPrompt calls
			// so that AppendedSystemPrompts is fully populated before the registry rebuild
			// re-assembles the final system prompt.
			api.activeSessionsMux.RLock()
			memFolderForPrompt := ""
			llmGuidance := ""
			if sess, ok := api.activeSessions[sessionID]; ok {
				memFolderForPrompt = sess.MemoryFolder
				llmGuidance = sess.LLMGuidance
			}
			api.activeSessionsMux.RUnlock()

			// ── PROMPT ASSEMBLY ORDER ──
			// Priority-ordered: operating mode → workspace map → context → mode-specific → reference docs.
			// This ensures the LLM sees its core behavior rules before any reference material.

			shellRoot := fsutil.WorkspaceShellRoot()

			// 1. OPERATING MODE — the agent's core behavior (delegate everything vs work directly).
			//    This MUST come first so it takes precedence over reference material.
			if !isWorkflowPhase {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetMultiAgentDelegationInstructionsWithUser(perUserChatsFolder, currentUserID))
				logfWithContext(queryLogCtx, "[DELEGATION] Added multi-agent delegation instructions to system prompt")
				if section := virtualtools.BuildSpawnCapabilitiesSection(buildCapabilitiesContext(req)); section != "" {
					underlyingAgent.AppendSystemPrompt(section)
				}
				if delegationTierCfg := resolveDelegationTierConfig(req.DelegationTierConfig); delegationTierCfg != nil {
					if tierSection := virtualtools.BuildCustomTierPromptSection(delegationTierCfg); tierSection != "" {
						underlyingAgent.AppendSystemPrompt(tierSection)
					}
				}
			}

			// 2. WORKSPACE MAP — compact folder listing with absolute paths and access levels.
			if isWorkflowPhase {
				underlyingAgent.AppendSystemPrompt(GetWorkflowPhaseWorkspaceMap(shellRoot, workflowPhaseFolder, perUserMemoryFolder))
			} else {
				underlyingAgent.AppendSystemPrompt(GetWorkspaceMap(shellRoot, perUserChatsFolder, perUserMemoryFolder))
			}
			if capabilitySection := buildLLMCapabilityPromptSection(r.Context()); capabilitySection != "" {
				underlyingAgent.AppendSystemPrompt(capabilitySection)
				log.Printf("[LLM TOOLS] Added LLM/media capability snapshot to system prompt")
			}

			// 3. CONTEXT — employees, workflow references, skills (what the agent needs to know).
			if !isWorkflowPhase {
				if empSection := buildEmployeesWorkflowsContext(); empSection != "" {
					underlyingAgent.AppendSystemPrompt(empSection)
					log.Printf("[EMPLOYEES] Injected employees & workflow assignments into system prompt")
				}
			}
			if len(req.WorkflowContextPaths) > 0 {
				if workflowPrompt := buildWorkflowContextPrompt(req.WorkflowContextPaths, getWorkspaceAPIURL()); workflowPrompt != "" {
					underlyingAgent.AppendSystemPrompt(workflowPrompt)
					log.Printf("[WORKFLOW-CTX] Added workflow context to system prompt (%d workflows)", len(req.WorkflowContextPaths))
				}
			}
			if len(req.SelectedSkills) > 0 {
				if skillPrompt := buildSkillPrompt(req.SelectedSkills, getWorkspaceAPIURL(), shellRoot, isMultiAgentChat); skillPrompt != "" {
					underlyingAgent.AppendSystemPrompt(skillPrompt)
					log.Printf("[SKILLS] Added skill instructions to system prompt (%d skills)", len(req.SelectedSkills))
				}
			}

			// Channel formatting rules — tell the agent which markup subset
			// the bot platform renders, so replies don't arrive with stray
			// "## Headers" or "[link](url)" syntax that WhatsApp / Slack
			// display literally. No-op when BotPlatform is empty (chat UI).
			if channelPrompt := buildChannelFormattingInstructions(req.BotPlatform); channelPrompt != "" {
				underlyingAgent.AppendSystemPrompt(channelPrompt)
				log.Printf("[CHANNEL] Added %s formatting rules to system prompt", req.BotPlatform)
			}

			// 4. MODE-SPECIFIC — browser, GWS, memory (only when those capabilities are active).
			chatBrowserCfg := buildChatBrowserConfig(req)
			if chatBrowserPrompt := browserinstructions.BuildBrowserInstructions(chatBrowserCfg); chatBrowserPrompt != "" {
				underlyingAgent.AppendSystemPrompt(chatBrowserPrompt)
				log.Printf("[BROWSER] Added browser instructions to system prompt (playwright=%v, agent-browser=%v, cdp=%v)",
					chatBrowserCfg.HasPlaywright, chatBrowserCfg.HasAgentBrowser, chatBrowserCfg.CdpPort > 0)
			}
			if req.EnableGWSAccess != nil && *req.EnableGWSAccess {
				underlyingAgent.AppendSystemPrompt(getGWSQuickStartInstructions())
				log.Printf("[GWS] Added GWS quick-start instructions to system prompt")
			}
			underlyingAgent.AppendSystemPrompt(virtualtools.GetMemoryInstructions(memFolderForPrompt))

			// Auto-inject memory index.md so the agent has prior context without needing a tool call.
			// This is critical for the orchestrator which would otherwise skip recall_memory entirely.
			{
				indexPath := perUserMemoryFolder + "/index.md"
				if indexContent, exists, err := readFileFromWorkspace(context.Background(), indexPath); err == nil && exists && indexContent != "" {
					// Truncate very large indices to avoid bloating the prompt
					if len(indexContent) > 4000 {
						indexContent = indexContent[:4000] + "\n\n... (truncated — use recall_memory for full details)"
					}
					underlyingAgent.AppendSystemPrompt("\n## Your Memory (auto-loaded)\n\n" + indexContent)
					log.Printf("[MEMORY] Auto-injected index.md (%d chars) into system prompt", len(indexContent))
				}
			}

			// 5. REFERENCE DOCS — detailed config schemas, workflow structure, workflow creation.
			//    Only for worker agents (workflow phase). The orchestrator delegates all file
			//    work so it doesn't need 300+ lines of config schemas and parsing commands.
			if isWorkflowPhase {
				underlyingAgent.AppendSystemPrompt(GetWorkspaceReference(shellRoot, perUserChatsFolder, perUserMemoryFolder))
			}

			// 6. SUPPLEMENTARY — conditional grants, CLI provider overrides.
			for _, section := range resolvedGrants.PromptSections {
				underlyingAgent.AppendSystemPrompt(section)
			}
			if len(resolvedGrants.PromptSections) > 0 {
				log.Printf("[GRANTS] Appended %d prompt section(s) for active grants: %v", len(resolvedGrants.PromptSections), resolvedGrants.AppliedNames)
			}

			// Update code execution registry AFTER all AppendSystemPrompt calls so that
			// AppendedSystemPrompts is fully populated. rebuildSystemPromptWithUpdatedToolStructure
			// will then re-assemble the final prompt as: (clean base with tool structure) + all appended prompts.
			if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
				log.Printf("[CUSTOM TOOLS] Warning: Failed to update code execution registry: %v", err)
			}

			log.Printf("[SYSTEM_PROMPT] Final assembled prompt length=%d chars, hasGuidance=%v", len(underlyingAgent.GetSystemPrompt()), req.LLMGuidance != "" || llmGuidance != "")

			// Add CLI-specific tool mapping for providers that use the api-bridge.
			// These providers can only call mcp__api-bridge__* tools directly;
			// delegation/memory tools must be called via curl through execute_shell_command.
			if common.IsCLIProvider(req.Provider) {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[CLI PROVIDER] Added custom tool HTTP API mapping for %s", req.Provider)
			}
			// [BROWSER_UPLOAD] Inject file upload instructions into the agent's system prompt
			// and register the path transformer on the agent itself (primary interception point).
			// Two conditions trigger this: headless browser (agent_browser) or Playwright MCP.
			// The system prompt tells the LLM to use workspace-relative paths; the transformer
			// then resolves those to absolute host paths before they reach Playwright MCP.
			// [BROWSER_UPLOAD] Register file path transformer for browser file uploads.
			// Browser instructions (upload + mode-specific) are already injected above via BuildBrowserInstructions.
			hasBrowserAccess := req.EnableBrowserAccess != nil && *req.EnableBrowserAccess
			hasPlaywright := false
			for _, s := range req.EnabledServers {
				if s == "playwright" {
					hasPlaywright = true
				}
			}
			if hasBrowserAccess || hasPlaywright {
				// Register transformer on the agent (primary path for LLM-driven tool calls).
				// Agent tool calls go through conversation.go → toolArgTransformers, NOT through
				// the HTTP /api/mcp/execute handler. Without this, the transformer never fires.
				{
					wsAbsPath := fsutil.WorkspaceShellRoot()
					underlyingAgent.SetToolArgTransformer("browser_file_upload", func(args map[string]interface{}) {
						paths, ok := args["paths"].([]interface{})
						if !ok || len(paths) == 0 {
							log.Printf("[BROWSER_UPLOAD] No paths in args or wrong type, skipping transform")
							return
						}
						for i, p := range paths {
							pathStr, ok := p.(string)
							if !ok || pathStr == "" || filepath.IsAbs(pathStr) {
								log.Printf("[BROWSER_UPLOAD] Skipping path[%d]=%q (abs or empty)", i, p)
								continue
							}
							resolved := filepath.Join(wsAbsPath, pathStr)
							log.Printf("[BROWSER_UPLOAD] Resolved path[%d]: %q -> %q", i, pathStr, resolved)
							paths[i] = resolved
						}
					})
					log.Printf("[BROWSER_UPLOAD] Registered agent-level browser_file_upload transformer, workspace=%s", wsAbsPath)
				}
			}

			// --- Workflow Phase Chat Mode ---
			// Override system prompt and register plan modification tools for conversational phase editing
			if isWorkflowPhase && workflowPhaseID != "" {
				log.Printf("[WORKFLOW_PHASE] Setting up phase chat mode: phase=%s preset=%s", workflowPhaseID, req.PresetQueryID)

				// Get workspace path and objective from preset or request
				phaseWorkspacePath := ""
				phaseObjective := ""
				// For scheduler/cron triggers, the workspace path comes from selected_folder
				// and preset may not exist in the DB. Use selected_folder as primary source.
				if req.SelectedFolder != "" {
					phaseWorkspacePath = req.SelectedFolder
				}
				// Resolve workspace path from manifest if not already set
				if phaseWorkspacePath == "" && req.PresetQueryID != "" {
					if p, e := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); e == nil && p != "" {
						phaseWorkspacePath = p
					}
				}
				// Load objective from manifest label
				if phaseWorkspacePath != "" && phaseObjective == "" {
					if manifest, found, mErr := ReadWorkflowManifest(context.Background(), phaseWorkspacePath); mErr == nil && found {
						phaseObjective = manifest.Label
					}
				}
				if phaseWorkspacePath == "" {
					// Fallback: try to extract workspace path from the query's file context marker
					phaseWorkspacePath = extractWorkspacePathFromObjective(req.Query)
				}
				if phaseWorkspacePath == "" {
					log.Printf("[WORKFLOW_PHASE] WARNING: No workspace path found for phase=%s preset=%s - using default_workspace", workflowPhaseID, req.PresetQueryID)
					phaseWorkspacePath = "default_workspace"
				}
				// Set default shell working directory for this session.
				// The global map is read by execute_shell_command at call time.
				if phaseWorkspacePath != "" && phaseWorkspacePath != "default_workspace" {
					workspace.SetSessionWorkingDir(sessionID, phaseWorkspacePath)
					// Restrict shell commands to the workflow folder via Isolator
					// Include #workflow read-only paths so the builder can read referenced workflows
					phaseReadPaths := []string{phaseWorkspacePath, "Chats", "skills", "subagents", "Downloads"}
					phaseReadPaths = append(phaseReadPaths, workflowReadOnlyFolders...)
					workspace.SetSessionFolderGuard(sessionID,
						phaseReadPaths,
						[]string{phaseWorkspacePath, "Downloads"},
					)
					if len(workflowReadOnlyFolders) > 0 {
						log.Printf("[WORKFLOW_PHASE] Added read-only access for #workflow references: %v", workflowReadOnlyFolders)
					}
				}

				// Create workspace client for reading plan.json and variables.json
				phaseWSClient := workspace.NewClient(
					getWorkspaceAPIURL(),
					workspace.WithUserID(currentUserID),
				)

				// readFile closure: reads file content from workspace
				phaseReadFile := func(ctx context.Context, filePath string) (string, error) {
					result, err := phaseWSClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: filePath})
					if err != nil {
						return "", err
					}
					return result.Content, nil
				}

				// writeFile closure: writes content to workspace
				phaseWriteFile := func(ctx context.Context, filePath string, content string) error {
					_, err := phaseWSClient.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{Filepath: filePath, Content: content})
					return err
				}

				// moveFile closure: moves file in workspace
				phaseMoveFile := func(ctx context.Context, src string, dst string) error {
					_, err := phaseWSClient.MoveWorkspaceFile(ctx, workspace.MoveWorkspaceFileParams{SourceFilepath: src, DestinationFilepath: dst})
					return err
				}

				// Build template vars by reading current plan and variables from workspace
				phaseRunFolder := "iteration-0"
				var phaseEnabledGroupNames []string
				if req.ExecutionOptions != nil {
					phaseEnabledGroupNames = req.ExecutionOptions.EnabledGroupNames
				}
				// All workshop agents now run in code-execution mode regardless of
				// provider — there is no longer a tool-search / simple-agent path.
				// Provider-specific CLI handling (prompt template, api-bridge tool
				// mapping, native context) is decided separately via
				// common.IsCLIProvider.
				phaseIsCodeExec := true
				log.Printf("[WORKFLOW_PHASE] Mode detection: finalProvider=%q, isCodeExec=%v (always true)", finalProvider, phaseIsCodeExec)
				phaseTemplateVars := map[string]string{
					"Objective":           phaseObjective,
					"WorkspacePath":       phaseWorkspacePath,
					"IsCodeExecutionMode": fmt.Sprintf("%v", phaseIsCodeExec),
				}

				// Pass workshop mode from frontend override (auto-detection happens after plan is loaded below).
				// Migrate legacy values to the current 4-mode scheme
				// (builder/optimizer/run/reporting). 'ask'/'debugger'/'runner' fold
				// into 'run'; 'eval'/'output' fold into 'builder'. Reporting was
				// split out of builder/optimizer in a later release. Anything else
				// passes through so unknown future modes don't break old sessions.
				if req.ExecutionOptions != nil && req.ExecutionOptions.WorkshopMode != "" {
					mode := req.ExecutionOptions.WorkshopMode
					switch mode {
					case "ask", "debugger", "runner":
						mode = "run"
					case "eval", "output":
						mode = "builder"
					}
					phaseTemplateVars["WorkshopMode"] = mode
					log.Printf("[WORKSHOP_MODE] Using frontend override: %s (raw=%s)", mode, req.ExecutionOptions.WorkshopMode)
				}

				// Build GroupInfo and extra template vars for the interactive-workshop system prompt
				if workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder {
					groupInfo := buildWorkshopGroupInfo(r.Context(), phaseWorkspacePath, phaseReadFile, phaseRunFolder, phaseEnabledGroupNames)
					if groupInfo != "" {
						phaseTemplateVars["GroupInfo"] = groupInfo
					}
					phaseTemplateVars["RunFolder"] = phaseRunFolder
					phaseTemplateVars["UseKnowledgebase"] = "true"                 // default; overridden by preset below if needed
					phaseTemplateVars["KBShape"] = workflowtypes.KBShapeGraphNotes // default; overridden from manifest below if set
					if phaseWorkspacePath != "" {
						if manifest, found, mErr := ReadWorkflowManifest(context.Background(), phaseWorkspacePath); mErr == nil && found && manifest.Capabilities.LLMConfig != nil {
							if manifest.Capabilities.LLMConfig.KBShape != "" {
								phaseTemplateVars["KBShape"] = workflowtypes.ResolveKBShape(manifest.Capabilities.LLMConfig.KBShape)
							}
						}
					}
				}

				// Use a detached context for workspace file reads during setup so that
				// SSE streaming or other concurrent request activity cannot cancel them.
				// context.WithoutCancel preserves values (user ID, tracing) but drops the
				// cancellation signal, which is safe for these short, bounded reads.
				setupCtx := context.WithoutCancel(r.Context())

				// Read existing plan from workspace (if any)
				existingPlanJSON := todo_creation_human.ReadPlanFromWorkspace(setupCtx, phaseWorkspacePath, phaseReadFile)
				if existingPlanJSON != "" {
					phaseTemplateVars["ExistingPlanJSON"] = existingPlanJSON
					log.Printf("[WORKFLOW_PHASE] Loaded existing plan (%d bytes)", len(existingPlanJSON))

					// Extract compact step summary for builder-style phase prompts
					if stepSummary := extractStepSummary(existingPlanJSON); stepSummary != "" {
						phaseTemplateVars["StepSummary"] = stepSummary
						log.Printf("[WORKFLOW_PHASE] Extracted step summary (%d steps)", strings.Count(stepSummary, "\n"))
					}
				}

				// Extract workflow objective and success_criteria from soul/soul.md (the
				// canonical source; plan.json no longer holds these fields). Falls back
				// to workflow.json — see ResolveWorkflowObjective in soul_helpers.go for
				// the same resolution order the runtime uses.
				if workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder {
					objective, successCriteria, _ := todo_creation_human.ReadWorkflowObjectiveFromSoul(setupCtx, phaseWorkspacePath, phaseReadFile)
					if strings.TrimSpace(objective) == "" || strings.TrimSpace(successCriteria) == "" {
						// Legacy fallback to workflow.json root fields.
						if manifest, err := phaseReadFile(setupCtx, phaseWorkspacePath+"/workflow.json"); err == nil {
							var wf struct {
								Objective       string `json:"objective"`
								SuccessCriteria string `json:"success_criteria"`
							}
							if json.Unmarshal([]byte(manifest), &wf) == nil {
								if objective == "" {
									objective = wf.Objective
								}
								if successCriteria == "" {
									successCriteria = wf.SuccessCriteria
								}
							}
						}
					}
					if objective != "" {
						phaseTemplateVars["WorkflowObjective"] = objective
					}
					if successCriteria != "" {
						phaseTemplateVars["WorkflowSuccessCriteria"] = successCriteria
					}
				}

				// Default workshop mode if not provided by frontend. Run/Optimizer/Reporting
				// are explicit user/frontend choices, not inferred from step flags.
				if phaseTemplateVars["WorkshopMode"] == "" && existingPlanJSON != "" && workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder {
					phaseTemplateVars["WorkshopMode"] = "builder"
					log.Printf("[WORKSHOP_MODE] Defaulted to builder")
				}
				if phaseTemplateVars["WorkshopMode"] == "" {
					phaseTemplateVars["WorkshopMode"] = "builder"
				}

				// Read variable names from workspace (if any)
				variableNames := todo_creation_human.ReadVariablesFromWorkspace(setupCtx, phaseWorkspacePath, phaseReadFile)
				if variableNames != "" {
					phaseTemplateVars["VariableNames"] = variableNames
					log.Printf("[WORKFLOW_PHASE] Loaded variable names")
				}

				// Load workflow memory from memory/ folder (user-saved knowledge for this workflow)
				if phaseWorkspacePath != "" {
					memoryContent := loadWorkflowMemory(phaseWorkspacePath, phaseReadFile, setupCtx)
					if memoryContent != "" {
						phaseTemplateVars["CustomInstructions"] = memoryContent
						log.Printf("[WORKFLOW_PHASE] Loaded workflow memory (%d bytes)", len(memoryContent))
					}
				}

				// Generate phase-specific system prompt (dispatches by phaseId)
				phaseSystemPrompt := todo_creation_human.PhaseChatSystemPrompt(workflowPhaseID, phaseTemplateVars)

				// Append code execution / tool search instructions from mcpagent.
				// These tell the LLM HOW to call tools (via HTTP API, get_api_spec, etc.)
				// Without these, the LLM guesses parameter names instead of discovering them.
				if phaseIsCodeExec {
					codeExecInstructions := prompt.GetCodeExecutionInstructions(phaseWorkspacePath)
					phaseSystemPrompt += "\n\n" + codeExecInstructions
				}

				// Override the agent's system prompt — use SetSystemPrompt to properly set tracking flags
				// so that rebuildSystemPromptWithUpdatedToolStructure preserves this prompt
				underlyingAgent.ClearAppendedSystemPrompts()
				underlyingAgent.SetSystemPrompt(phaseSystemPrompt)
				log.Printf("[WORKFLOW_PHASE] Overrode system prompt (%d chars) for phase=%s", len(phaseSystemPrompt), workflowPhaseID)

				// Re-append supplementary prompts after system prompt override
				// (ClearAppendedSystemPrompts above wiped browser/GWS/secrets instructions)
				if capabilitySection := buildLLMCapabilityPromptSection(r.Context()); capabilitySection != "" {
					underlyingAgent.AppendSystemPrompt(capabilitySection)
					log.Printf("[WORKFLOW_PHASE] Appended LLM/media capability snapshot to %s system prompt", workflowPhaseID)
				}
				if workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder || workflowPhaseID == workflowtypes.WorkflowStatusEvalBuilder {
					// Secrets
					phaseSecrets := mergeGlobalSecrets(req.DecryptedSecrets, req.SelectedGlobalSecrets)
					if len(phaseSecrets) > 0 {
						entries := make([]orchestrator.SecretEntry, len(phaseSecrets))
						for i, s := range phaseSecrets {
							entries[i] = orchestrator.SecretEntry{Name: s.Name, Value: s.Value}
						}
						secretPrompt := todo_creation_human.BuildWorkflowSecretPrompt(entries)
						if secretPrompt != "" {
							underlyingAgent.AppendSystemPrompt(secretPrompt)
							log.Printf("[WORKFLOW_PHASE] Appended %d secrets to %s system prompt", len(entries), workflowPhaseID)
						}
					}

					// Browser + GWS instructions from manifest config
					// The manifest determines browser mode, not req.EnableBrowserAccess (which is false for workflow_phase)
					if phaseWorkspacePath != "" {
						phaseManifest, phaseFound, phaseMErr := ReadWorkflowManifest(context.Background(), phaseWorkspacePath)
						if phaseMErr == nil && phaseFound {
							effectiveBrowserMode := phaseManifest.Capabilities.BrowserMode

							// Build browser config from preset's browser mode
							phaseBrowserCfg := browserinstructions.BrowserConfig{}
							switch effectiveBrowserMode {
							case "cdp":
								phaseBrowserCfg.HasAgentBrowser = true
								phaseBrowserCfg.CdpPort = 9222 // Default CDP port (stored in preset, not req)
							case "headless":
								phaseBrowserCfg.HasAgentBrowser = true
							case "playwright":
								phaseBrowserCfg.HasPlaywright = true
							}
							// Also check selectedServers for Playwright (may be set independently)
							for _, s := range selectedServers {
								switch s {
								case "playwright":
									phaseBrowserCfg.HasPlaywright = true
								}
							}
							if phaseBrowserPrompt := browserinstructions.BuildBrowserInstructions(phaseBrowserCfg); phaseBrowserPrompt != "" {
								underlyingAgent.AppendSystemPrompt(phaseBrowserPrompt)
								log.Printf("[WORKFLOW_PHASE] Appended browser instructions to %s (mode=%s, playwright=%v, agent-browser=%v)",
									workflowPhaseID, effectiveBrowserMode, phaseBrowserCfg.HasPlaywright, phaseBrowserCfg.HasAgentBrowser)
							}

							// Register agent_browser tool on the chat agent for headless/CDP modes.
							// Without this, the MCP bridge can't find agent_browser and the LLM
							// falls back to calling agent-browser via execute_shell_command (which bypasses CDP resolution).
							if phaseBrowserCfg.HasAgentBrowser {
								phaseCdpPort := 0
								if effectiveBrowserMode == "cdp" {
									phaseCdpPort = 9222
								}
								phaseBrowserTools := virtualtools.CreateWorkspaceBrowserTools()
								phaseBrowserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, phaseCdpPort)
								phaseBrowserCategory := virtualtools.GetWorkspaceBrowserToolCategory()
								for _, tool := range phaseBrowserTools {
									if tool.Function == nil {
										continue
									}
									if executor, exists := phaseBrowserExecutors[tool.Function.Name]; exists {
										var params map[string]interface{}
										if tool.Function.Parameters != nil {
											paramsBytes, _ := json.Marshal(tool.Function.Parameters)
											json.Unmarshal(paramsBytes, &params)
										}
										if params != nil {
											if err := underlyingAgent.RegisterCustomTool(
												tool.Function.Name,
												tool.Function.Description,
												params,
												executor,
												phaseBrowserCategory,
											); err != nil {
												log.Printf("[WORKFLOW_PHASE] Warning: Failed to register browser tool %s: %v", tool.Function.Name, err)
											} else {
												log.Printf("[WORKFLOW_PHASE] Registered browser tool: %s (category: %s, cdp_port=%d)", tool.Function.Name, phaseBrowserCategory, phaseCdpPort)
											}
										}
									}
								}
							}

							// GWS instructions (check if gws server is in selected servers)
							for _, s := range selectedServers {
								if s == "gws" {
									underlyingAgent.AppendSystemPrompt(browserinstructions.GetGWSQuickStartInstructions())
									log.Printf("[WORKFLOW_PHASE] Appended GWS instructions to %s", workflowPhaseID)
									break
								}
							}
						}
					}
				}

				// Re-append workflow context prompt for #workflow references
				// (was wiped by ClearAppendedSystemPrompts above)
				if len(req.WorkflowContextPaths) > 0 {
					workflowPrompt := buildWorkflowContextPrompt(req.WorkflowContextPaths, getWorkspaceAPIURL())
					if workflowPrompt != "" {
						underlyingAgent.AppendSystemPrompt(workflowPrompt)
						log.Printf("[WORKFLOW_PHASE] Re-appended workflow context prompt (%d workflows) after system prompt override", len(req.WorkflowContextPaths))
					}
				}

				// Register phase-appropriate tools
				switch workflowPhaseID {
				case workflowtypes.WorkflowStatusWorkflowBuilder:
					// Plan modification tools + workshop execution tools (execute_step, query_step, stop_step, etc.)
					// FATAL on failure: the workflow-builder system prompt advertises these tools,
					// so a half-registered builder silently hallucinates missing tools to the LLM.
					// Schemas are covered by TestAllSchemaFunctionsReturnValidJSON — this should
					// never fire in a healthy build.
					if err := todo_creation_human.RegisterPlanModificationTools(
						underlyingAgent,
						phaseWorkspacePath,
						api.logger,
						phaseReadFile,
						phaseWriteFile,
						phaseMoveFile,
						fmt.Sprintf("%s chat agent", workflowPhaseID),
					); err != nil {
						log.Fatalf("[WORKFLOW_PHASE] FATAL: Failed to register plan modification tools for workflow-builder: %v", err)
					}
					log.Printf("[WORKFLOW_PHASE] Registered plan modification tools for %s", workflowPhaseID)

					// STOP-RACE GUARD: Check if the session was stopped while this goroutine
					// was in flight. Without this check, the goroutine would create a new
					// WorkshopChatSession with a fresh context.Background() that is never
					// canceled, leaving orphaned CLI processes running indefinitely.
					// This was the root cause of the 2026-04-04 "can't stop" bug.
					if api.isSessionMarkedStopped(sessionID) {
						log.Printf("[WORKFLOW_PHASE] Session %s was stopped — aborting workshop creation to prevent orphaned processes", sessionID)
						return
					}

					// Get or create per-session workshop controller + step registry
					workshopSessionKey := sessionID
					var workshopSession *todo_creation_human.WorkshopChatSession
					if cached, ok := api.workshopChatSessions.Load(workshopSessionKey); ok {
						workshopSession = cached.(*todo_creation_human.WorkshopChatSession)
						log.Printf("[WORKFLOW_PHASE] Reusing existing workshop session for %s", sessionID)

						// Always refresh API keys on session reuse (workspace keys may have changed)
						// Use mergedAPIKeys loaded before goroutine (r.Context() is canceled inside goroutine)
						workshopSession.UpdateAPIKeys(mergedAPIKeys)

						// Refresh enabled group IDs from current request (toolbar selection may have changed)
						if req.ExecutionOptions != nil && len(req.ExecutionOptions.EnabledGroupNames) > 0 {
							workshopSession.UpdateEnabledGroupNames(r.Context(), req.ExecutionOptions.EnabledGroupNames)
							log.Printf("[WORKFLOW_PHASE] Refreshed enabled group names: %v", req.ExecutionOptions.EnabledGroupNames)
						}

						// Pass frontend-selected workshop mode so AUTO-NOTIFICATION action hints use the correct mode
						if req.ExecutionOptions != nil && req.ExecutionOptions.WorkshopMode != "" {
							workshopSession.SetWorkshopModeOverride(req.ExecutionOptions.WorkshopMode)
						}

						// Refresh all settings from manifest in case user edited the workflow
						if phaseWorkspacePath != "" {
							refreshManifest, refreshFound, refreshErr := ReadWorkflowManifest(context.Background(), phaseWorkspacePath)
							if refreshErr != nil {
								log.Printf("[WORKFLOW_PHASE] Warning: Failed to reload manifest: %v", refreshErr)
							} else if refreshFound {
								caps := refreshManifest.Capabilities
								selectedServers = caps.SelectedServers

								refreshedTools := caps.SelectedTools
								toolsParsed := true
								refreshedSkills := caps.SelectedSkills
								skillsParsed := true

								// Refresh secrets
								refreshedUserSecrets := api.loadSelectedUserSecrets(context.Background(), currentUserID, caps.SelectedSecrets)
								effectiveSecretSelection := req.SelectedGlobalSecrets
								if caps.SelectedGlobalSecretNames != nil {
									effectiveSecretSelection = caps.SelectedGlobalSecretNames
								}
								allRefreshedSecrets := mergeGlobalSecrets(refreshedUserSecrets, effectiveSecretSelection)
								var secretEntries []orchestrator.SecretEntry
								for _, s := range allRefreshedSecrets {
									secretEntries = append(secretEntries, orchestrator.SecretEntry{Name: s.Name, Value: s.Value})
								}

								// LLM config
								refreshedKnowledgebase := true
								refreshedLockKnowledgebase := false
								log.Printf("[WORKFLOW_PHASE] Refresh LLMConfig: isNil=%v", caps.LLMConfig == nil)
								if caps.LLMConfig != nil {
									log.Printf("[WORKFLOW_PHASE] Refresh LLMConfig details: allocationMode=%q tieredConfig=%v",
										caps.LLMConfig.LLMAllocationMode, caps.LLMConfig.TieredConfig != nil)
									phaseLLM := workshopExtractLLM(caps.LLMConfig.PhaseLLM, caps.LLMConfig.Provider, caps.LLMConfig.ModelID)
									workshopSession.UpdatePresetLLMConfigs(phaseLLM)

									if caps.LLMConfig.TieredConfig != nil {
										refreshedTiered := workshopConvertTieredLLMConfig(caps.LLMConfig.TieredConfig)
										workshopSession.UpdateTieredConfig(refreshedTiered)
										log.Printf("[WORKFLOW_PHASE] Refreshed tiered config from manifest")
									} else {
										workshopSession.UpdateTieredConfig(nil)
									}

									if caps.LLMConfig.UseKnowledgebase != nil {
										refreshedKnowledgebase = *caps.LLMConfig.UseKnowledgebase
									}
									if caps.LLMConfig.LockKnowledgebase != nil {
										refreshedLockKnowledgebase = *caps.LLMConfig.LockKnowledgebase
									}
								}

								workshopSession.UpdatePresetSettings(
									selectedServers,
									refreshedTools, toolsParsed,
									caps.UseCodeExecutionMode,
									refreshedKnowledgebase,
									refreshedLockKnowledgebase,
									refreshedSkills, skillsParsed,
									secretEntries,
								)
								log.Printf("[WORKFLOW_PHASE] Refreshed settings from manifest: servers=%d tools=%d codeExec=%v kb=%v kbLock=%v skills=%d secrets=%d",
									len(selectedServers), len(refreshedTools), caps.UseCodeExecutionMode,
									refreshedKnowledgebase, refreshedLockKnowledgebase, len(refreshedSkills), len(secretEntries))
							}
						}
					} else {
						// Build full workshop config matching normal workflow setup
						workshopCfg, cfgErr := api.buildWorkshopConfig(r.Context(), req, currentUserID, phaseWorkspacePath, phaseRunFolder, selectedServers, sessionID, mergedAPIKeys)
						if cfgErr != nil {
							log.Printf("[WORKFLOW_PHASE] Error: Failed to build workshop config for %s: %v — workshop execution tools unavailable", workflowPhaseID, cfgErr)
						} else {
							newSession, sessionErr := todo_creation_human.NewWorkshopChatSession(r.Context(), workshopCfg)
							if sessionErr != nil {
								log.Printf("[WORKFLOW_PHASE] Warning: Failed to create workshop session for %s: %v — workshop execution tools unavailable", workflowPhaseID, sessionErr)
							} else {
								workshopSession = newSession
								api.workshopChatSessions.Store(workshopSessionKey, workshopSession)
								log.Printf("[WORKFLOW_PHASE] Created new %s session for %s", workflowPhaseID, sessionID)
							}
						}
					}

					if workshopSession != nil {
						workshopSession.SetExtraSubAgentNotifier(&workflowSubAgentTrackingNotifier{
							api:       api,
							sessionID: sessionID,
						})
						// NOTE: Do NOT wire todoSubAgentBgNotifier here. In workshop mode, all subAgentNotifier
						// calls come from sub-agents within background step executions (execute_step goroutines),
						// never from the main workshop agent itself. Wiring todoSubAgentBgNotifier causes double
						// AUTO-NOTIFICATIONs: one for each todo sub-agent AND one for the exec-* completion.
						// The workshopExecutionBgNotifier below is the single source of auto-notifications.
						//
						// Wire workshop execution notifier so execute_step/run_in_background/harden_workflow
						// register in bgAgentRegistry (keeps frontend polling alive while background executions run).
						workshopSession.SetWorkshopExecutionNotifier(&workshopExecutionBgNotifier{
							api:           api,
							sessionID:     sessionID,
							workspacePath: phaseWorkspacePath,
							presetQueryID: req.PresetQueryID,
							userID:        currentUserID,
						})
						workshopSession.SetExecutionStateChecks(
							func() bool {
								api.pendingMu.RLock()
								defer api.pendingMu.RUnlock()
								return len(api.pendingCompletions[sessionID]) > 0
							},
							func() bool { return api.bgAgentRegistry.HasRunningAgents(sessionID) },
							func() { api.bgAgentRegistry.CancelAll(sessionID) },
							func() []todo_creation_human.ServerAgentInfo {
								agents := api.bgAgentRegistry.GetAll(sessionID)
								result := make([]todo_creation_human.ServerAgentInfo, 0, len(agents))
								for _, a := range agents {
									result = append(result, todo_creation_human.ServerAgentInfo{
										ID: a.ID, Name: a.Name, Status: string(a.GetStatus()),
									})
								}
								return result
							},
						)
						todo_creation_human.RegisterWorkshopChatTools(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered workshop execution tools for %s (execute_step, query_step, stop_step, list_steps, etc.)", workflowPhaseID)

						builderSession := workshopSession
						afterDelete := func(ctx context.Context, name string) error {
							if builderSession == nil {
								return nil
							}
							return builderSession.DetachSecretFromWorkflow(ctx, name)
						}
						if err := api.registerSecretManagementTools(underlyingAgent, currentUserID, "secret_tools", afterDelete); err != nil {
							log.Printf("[WORKFLOW_PHASE] Warning: Failed to register secret tools in %s: %v", workflowPhaseID, err)
						} else {
							log.Printf("[WORKFLOW_PHASE] Registered secret tools in %s (list_secrets, set_user_secret, delete_user_secret) with workflow auto-detach", workflowPhaseID)
						}
					}

					// Register evaluation tools in builder-style phases: validation plus
					// full execution against the current run.
					if err := todo_creation_human.RegisterEvaluationValidationTools(
						underlyingAgent,
						phaseWorkspacePath,
						api.logger,
						phaseReadFile,
						phaseWriteFile,
						phaseMoveFile,
					); err != nil {
						log.Printf("[WORKFLOW_PHASE] Warning: Failed to register evaluation validation tool in %s: %v", workflowPhaseID, err)
					} else {
						log.Printf("[WORKFLOW_PHASE] Registered evaluation validation tool in %s", workflowPhaseID)
					}

					if phaseTemplateVars["WorkshopMode"] == "builder" || phaseTemplateVars["WorkshopMode"] == "optimizer" || phaseTemplateVars["WorkshopMode"] == "reporting" {
						// Reporting tools: JSON report-plan read/write tools plus validation and
						// preview against real db/*.json / knowledgebase/*.json sources. The renderer
						// silently drops bad widgets, so validation stays in the loop.
						if err := todo_creation_human.RegisterReportPlanManagementTools(
							underlyingAgent,
							phaseWorkspacePath,
							api.logger,
							phaseReadFile,
							phaseWriteFile,
						); err != nil {
							log.Printf("[WORKFLOW_PHASE] Warning: Failed to register report plan management tools in %s: %v", workflowPhaseID, err)
						} else {
							log.Printf("[WORKFLOW_PHASE] Registered report plan management tools in %s", workflowPhaseID)
						}

						if err := todo_creation_human.RegisterReportPlanValidationTools(
							underlyingAgent,
							phaseWorkspacePath,
							api.logger,
							phaseReadFile,
						); err != nil {
							log.Printf("[WORKFLOW_PHASE] Warning: Failed to register report plan validation tool in %s: %v", workflowPhaseID, err)
						} else {
							log.Printf("[WORKFLOW_PHASE] Registered report plan validation tool in %s", workflowPhaseID)
						}

						if err := todo_creation_human.RegisterReportRenderPreviewTool(
							underlyingAgent,
							phaseWorkspacePath,
							api.logger,
							phaseReadFile,
						); err != nil {
							log.Printf("[WORKFLOW_PHASE] Warning: Failed to register report render preview tool in %s: %v", workflowPhaseID, err)
						} else {
							log.Printf("[WORKFLOW_PHASE] Registered report render preview tool in %s", workflowPhaseID)
						}
					} else {
						log.Printf("[WORKFLOW_PHASE] Skipped report plan tools in %s mode for %s", phaseTemplateVars["WorkshopMode"], workflowPhaseID)
					}

					// Create eval session for run_full_evaluation (needs isEvaluationMode=true)
					evalSessionKey := "eval-" + sessionID
					var evalSession *todo_creation_human.WorkshopChatSession
					if cached, ok := api.workshopChatSessions.Load(evalSessionKey); ok {
						evalSession = cached.(*todo_creation_human.WorkshopChatSession)
						log.Printf("[WORKFLOW_PHASE] Reusing existing eval session in %s %s", workflowPhaseID, sessionID)
					} else {
						evalCfg, evalCfgErr := api.buildWorkshopConfig(r.Context(), req, currentUserID, phaseWorkspacePath, phaseRunFolder, selectedServers, sessionID, mergedAPIKeys)
						if evalCfgErr != nil {
							log.Printf("[WORKFLOW_PHASE] Error: Failed to build eval config in %s: %v", workflowPhaseID, evalCfgErr)
						} else {
							evalCfg.IsEvaluationMode = true
							newEvalSession, evalSessionErr := todo_creation_human.NewWorkshopChatSession(r.Context(), evalCfg)
							if evalSessionErr != nil {
								log.Printf("[WORKFLOW_PHASE] Warning: Failed to create eval session in %s: %v", workflowPhaseID, evalSessionErr)
							} else {
								evalSession = newEvalSession
								api.workshopChatSessions.Store(evalSessionKey, evalSession)
								log.Printf("[WORKFLOW_PHASE] Created eval session in %s for %s", workflowPhaseID, sessionID)
							}
						}
					}
					if evalSession != nil {
						evalSession.SetExtraSubAgentNotifier(&workflowSubAgentTrackingNotifier{
							api:       api,
							sessionID: sessionID,
						})
						evalSession.SetWorkshopExecutionNotifier(&workshopExecutionBgNotifier{
							api:           api,
							sessionID:     sessionID,
							workspacePath: phaseWorkspacePath,
							presetQueryID: req.PresetQueryID,
							userID:        currentUserID,
						})
						evalSession.SetExecutionStateChecks(
							func() bool {
								api.pendingMu.RLock()
								defer api.pendingMu.RUnlock()
								return len(api.pendingCompletions[sessionID]) > 0
							},
							func() bool { return api.bgAgentRegistry.HasRunningAgents(sessionID) },
							func() { api.bgAgentRegistry.CancelAll(sessionID) },
							func() []todo_creation_human.ServerAgentInfo {
								agents := api.bgAgentRegistry.GetAll(sessionID)
								result := make([]todo_creation_human.ServerAgentInfo, 0, len(agents))
								for _, a := range agents {
									result = append(result, todo_creation_human.ServerAgentInfo{
										ID: a.ID, Name: a.Name, Status: string(a.GetStatus()),
									})
								}
								return result
							},
						)
						todo_creation_human.RegisterRunFullEvaluationTool(underlyingAgent, evalSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered run_full_evaluation in %s", workflowPhaseID)
					}
					if workshopSession != nil {
						todo_creation_human.RegisterRunFullWorkflowTool(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered run_full_workflow in %s", workflowPhaseID)
						todo_creation_human.RegisterImproveKnowledgebaseTool(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered improve_kb in %s", workflowPhaseID)
						todo_creation_human.RegisterReorganizeKnowledgebaseTool(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered reorganize_knowledgebase in %s", workflowPhaseID)
						todo_creation_human.RegisterConsolidateKnowledgebaseTool(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered consolidate_knowledgebase in %s", workflowPhaseID)
						// Auto-improvement metric tools mutate metrics, so keep
						// them in Optimizer mode. capture_context is also safe in
						// Run mode because it requires explicit user confirmation
						// and target metric anchoring.
						switch phaseTemplateVars["WorkshopMode"] {
						case "optimizer":
							RegisterAutoImprovementProposerTools(underlyingAgent, phaseWorkspacePath, "improve-workflow", api.logger)
							log.Printf("[WORKFLOW_PHASE] Registered auto-improvement proposer tools in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
						case "run":
							RegisterCaptureContextTool(underlyingAgent, phaseWorkspacePath, api.logger)
							log.Printf("[WORKFLOW_PHASE] Registered capture_context in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
						default:
							log.Printf("[WORKFLOW_PHASE] Skipped auto-improvement proposer tools in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
						}
						// Guided-flow text for every workflow slash command, returned via
						// get_workflow_command_guidance(kind=...). Available across modes;
						// per-kind mode validation lives in the tool itself.
						guidance.RegisterGuidanceTool(underlyingAgent, phaseTemplateVars["WorkshopMode"], api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered get_workflow_command_guidance in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
					}
				default:
					// planning: plan modification tools
					// FATAL on failure — see workflow-builder case above for rationale.
					if err := todo_creation_human.RegisterPlanModificationTools(
						underlyingAgent,
						phaseWorkspacePath,
						api.logger,
						phaseReadFile,
						phaseWriteFile,
						phaseMoveFile,
						fmt.Sprintf("%s chat agent", workflowPhaseID),
					); err != nil {
						log.Fatalf("[WORKFLOW_PHASE] FATAL: Failed to register plan modification tools for phase=%s: %v", workflowPhaseID, err)
					}
					log.Printf("[WORKFLOW_PHASE] Registered plan modification tools for phase=%s", workflowPhaseID)
				}

				// Apply per-turn tool allow list based on current workshop mode.
				// This restricts which tools the LLM can see/call, enforcing mode boundaries
				// (e.g. DEBUG mode cannot execute steps, BUILD mode cannot optimize).
				// The allow list is applied in conversation.go (filteredTools) and buildToolIndex() (code exec).
				if workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder {
					workshopMode := phaseTemplateVars["WorkshopMode"]
					allowedTools := todo_creation_human.GetToolsForWorkshopMode(workshopMode)
					underlyingAgent.SetToolAllowList(allowedTools)
					log.Printf("[WORKSHOP_TOOLS] Applied tool allow list for mode=%s (%d tools): %v", workshopMode, len(allowedTools), allowedTools)
				} else {
					// Non-workshop phases get all tools
					underlyingAgent.ClearToolAllowList()
				}

				// Rebuild code execution registry after prompt + tool changes
				if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
					log.Printf("[WORKFLOW_PHASE] Warning: Failed to update code execution registry: %v", err)
				}

				log.Printf("[WORKFLOW_PHASE] Phase chat setup complete: phase=%s workspace=%s", workflowPhaseID, phaseWorkspacePath)
			}
		}

		// Attach the in-memory event observer for real-time SSE/polling, plus
		// the cost-ledger observer that persists every token_usage event to
		// the global cost log.
		eventObserver := events.NewEventObserverWithLogger(api.eventStore, sessionID, api.logger)
		costObs := newCostObserver(api.costLedger, sessionID, currentUserID, req.AgentMode)
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			underlyingAgent.AddEventListener(eventObserver)
			underlyingAgent.AddEventListener(costObs)
			api.runningAgentsMux.Lock()
			api.runningAgents[sessionID] = underlyingAgent
			api.runningAgentsMux.Unlock()
		} else {
			log.Printf("[AGENT] ERROR: underlying MCP agent is nil for session %s", sessionID)
		}

		// Detect workshop-mode toggle since the previous turn. If the mode
		// changed, the saved CLI session IDs (gemini, claudeCode) are now
		// stale — they'd resume into a session whose system prompt and
		// allow-list reflect the previous mode. Drop them, then replace the
		// agent-replay history with a single synthetic recap so the new
		// agent sees just enough context to continue.
		//
		// Source: req.ExecutionOptions.WorkshopMode is the frontend-supplied
		// mode override; phaseTemplateVars (where the workflow branch above
		// stores the resolved mode) is out of scope here. Apply the same
		// legacy-value migration as that branch so old values from saved
		// sessions / stale schedule entries don't trigger spurious changes.
		newWorkshopMode := ""
		if req.ExecutionOptions != nil {
			newWorkshopMode = req.ExecutionOptions.WorkshopMode
			switch newWorkshopMode {
			case "ask", "debugger", "runner":
				newWorkshopMode = "run"
			case "eval", "output":
				newWorkshopMode = "builder"
			}
		}
		modeChangedThisTurn := false
		// Snapshot of the pre-mode-change history. When the user toggles mode
		// mid-session we replace api.conversationHistory with just a recap (so
		// the agent sees a tight context, not stale tool calls), but the on-
		// disk record should keep the full conversation. This snapshot is
		// merged with the new turn's exchange at save time below.
		var preModeChangeSnapshot []llmtypes.MessageContent
		if newWorkshopMode != "" {
			api.conversationMux.Lock()
			prevMode, hadPrev := api.lastWorkshopModeBySession[sessionID]
			if hadPrev && prevMode != "" && prevMode != newWorkshopMode {
				modeChangedThisTurn = true
				log.Printf("[WORKSHOP_MODE] Mode changed %q -> %q for session %s; dropping CLI session and rebuilding history as recap", prevMode, newWorkshopMode, sessionID)
				// Drop CLI session IDs so the next call starts fresh with the new prompt.
				delete(api.claudeCodeSessionIDs, sessionID)
				delete(api.geminiSessionIDs, sessionID)
				delete(api.geminiProjectDirIDs, sessionID)
				// Snapshot existing history before replacing — the on-disk
				// persisted record reuses this so the user sees a complete
				// conversation log, not a recap-only file.
				if existing, ok := api.conversationHistory[sessionID]; ok && len(existing) > 0 {
					preModeChangeSnapshot = make([]llmtypes.MessageContent, len(existing))
					copy(preModeChangeSnapshot, existing)
					// Replace history with a single synthetic user message containing
					// the recap. ~16000 chars ≈ 4000 tokens worth of recent context.
					recap := buildModeChangeRecap(existing, prevMode, newWorkshopMode, 16000)
					api.conversationHistory[sessionID] = []llmtypes.MessageContent{{
						Role:  llmtypes.ChatMessageTypeHuman,
						Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: recap}},
					}}
				}
			}
			api.lastWorkshopModeBySession[sessionID] = newWorkshopMode
			api.conversationMux.Unlock()
		}

		// Load conversation history for this session from the in-memory
		// cache. When the server restarts the cache is empty and the agent
		// starts a fresh conversation. After a mode-change above, this
		// replays just the synthetic recap message.
		api.conversationMux.RLock()
		if history, ok := api.conversationHistory[sessionID]; ok && len(history) > 0 {
			log.Printf("[CONVERSATION] Replaying %d in-memory messages for session %s", len(history), sessionID)
			for _, msg := range history {
				llmAgent.AppendMessage(msg)
			}
		}
		api.conversationMux.RUnlock()

		// Note: User message is added by StreamWithEvents internally, no need to add it here

		log.Printf("[AGENT DEBUG] Starting agent processing for query %s", queryID)

		// Create a cancellable context for agent execution using background context
		// This prevents the agent from being canceled when the HTTP request ends
		agentCtx, agentCancel := context.WithCancel(context.Background())

		// Inject user ID into the agent context
		agentCtx = context.WithValue(agentCtx, common.UserIDKey, currentUserID)
		agentCtx = context.WithValue(agentCtx, common.ChatSessionIDKey, sessionID)
		logfWithContext(queryLogCtx, "[USER_ID_DEBUGGING] Main agent: injected UserIDKey=%q, ChatSessionIDKey=%q into agentCtx", currentUserID, sessionID)

		// Store the cancel function for potential cancellation
		api.agentCancelMux.Lock()
		api.agentCancelFuncs[sessionID] = agentCancel
		api.agentCancelMux.Unlock()

		// Merge global secrets with user-supplied secrets, then inject into system prompt (not user message)
		chatQuery := req.Query

		// Skip secret prompt injection for workflow phases — they inject secrets in the phase setup above.
		// Only inject here for non-workflow chat agents (multi-agent, plain chat, etc.)
		isWorkflowPhase := req.PhaseID != ""
		allChatSecrets := mergeGlobalSecrets(req.DecryptedSecrets, req.SelectedGlobalSecrets)
		if len(allChatSecrets) > 0 && !isWorkflowPhase {
			// Inject secret values as environment variables for shell execution (SECRET_ prefix)
			for _, s := range allChatSecrets {
				workspaceEnv["SECRET_"+s.Name] = s.Value
			}
			logfWithContext(queryLogCtx, "[SECRETS] Injected %d secrets as environment variables for shell execution", len(allChatSecrets))

			// Only inject secret names (not values) into the system prompt — values are in env vars
			var secretNames []string
			for _, s := range allChatSecrets {
				secretNames = append(secretNames, "- `SECRET_"+s.Name+"` → accessible as `os.environ[\"SECRET_"+s.Name+"\"]` in Python or `$SECRET_"+s.Name+"` in bash")
			}
			secretPrompt := "\n## Secrets\n\nThe following secrets are available as environment variables in execute_shell_command. Do NOT ask the user for these values — read them from the environment.\n\n" + strings.Join(secretNames, "\n")
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.AppendSystemPrompt(secretPrompt)
				logfWithContext(queryLogCtx, "[SECRETS] Injected %d secret names (not values) into system prompt", len(allChatSecrets))
			}
		}

		// Restore Claude Code CLI session ID for --resume on subsequent turns
		if ccSID, ok := api.claudeCodeSessionIDs[sessionID]; ok {
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.ClaudeCodeSessionID = ccSID
			}
		}

		// Restore Gemini CLI session ID for --resume on subsequent turns
		if gSID, ok := api.geminiSessionIDs[sessionID]; ok {
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.GeminiSessionID = gSID
			}
		}

		// Restore Gemini CLI project dir ID for per-invocation isolation
		if gDirID, ok := api.geminiProjectDirIDs[sessionID]; ok {
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.GeminiProjectDirID = gDirID
				log.Printf("[GEMINI CLI] Restored project dir ID %s for session %s", gDirID, sessionID)
			}
			workspace.SetSessionGeminiProjectDirID(sessionID, gDirID)
		}

		// Store the fully configured agent before streaming starts so ultra-fast background
		// completions (for example learn_code fast-path runs) can trigger a synthetic turn
		// immediately. Waiting until the end of the first streamed turn creates a race where
		// the completion loop sees no stored agent and drops the auto-notification.
		{
			api.sessionAgentsMux.Lock()
			api.sessionAgents[sessionID] = llmAgent
			api.sessionAgentsMux.Unlock()
			log.Printf("[BG AGENT] Stored agent for session %s for synthetic turn reuse (pre-stream)", sessionID)
		}

		// Use the enhanced wrapper to get text chunks - events are handled via EventObserver and polling API
		logfWithContext(queryLogCtx, "[STREAMING_LIFECYCLE] T+%dms | Starting StreamWithEvents | session=%s query=%.80s", time.Since(startTime).Milliseconds(), sessionID, chatQuery)
		textChan, err := llmAgent.StreamWithEvents(agentCtx, chatQuery)
		if err != nil {
			logfWithContext(queryLogCtx, "[AGENT DEBUG] llmAgent.StreamWithEvents() error: %v", err)
			sendError(fmt.Sprintf("Failed to start streaming: %v", err), true)
			return
		}
		logfWithContext(queryLogCtx, "[LATENCY_DEBUG] T+%dms | StreamWithEvents channel opened | queryID=%s", time.Since(startTime).Milliseconds(), queryID)
		log.Printf("[AGENT DEBUG] llmAgent.StreamWithEvents() started successfully for query %s", queryID)

		// Stream response chunks with enhanced error handling
		chunkCount := 0

		log.Printf("[AGENT DEBUG] Entering streaming loop for query %s", queryID)
		for chunk := range textChan {
			log.Printf("[AGENT DEBUG] raw chunk (len=%d): %s", len(chunk), chunk)
			chunkCount++

			// Note: Chunks are processed by the agent internally, no manual accumulation needed

			// Save conversation history incrementally during streaming
			// This ensures we don't lose progress if streaming is stopped mid-way
			api.conversationMux.Lock()
			api.conversationHistory[sessionID] = llmAgent.GetHistory()
			api.conversationMux.Unlock()

			// Check for context cancellation
			select {
			case <-streamCtx.Done():
				tracer.EndTrace(traceID, map[string]interface{}{
					"status":   "timeout",
					"query_id": queryID,
				})

				// Update active session status to error
				api.updateSessionStatus(sessionID, "error")

				// Emit server-level timeout completion event
				// Create a timeout completion event using UnifiedCompletionEvent
				timeoutEventData := unifiedevents.NewUnifiedCompletionEventWithError(
					"server",              // agentType
					req.AgentMode,         // agentMode
					req.Query,             // question
					"context timeout",     // error message
					time.Since(startTime), // duration
					0,                     // turns
				)

				agentEvent := unifiedevents.NewAgentEvent(timeoutEventData)
				agentEvent.SessionID = sessionID

				serverTimeoutEvent := events.Event{
					ID:        fmt.Sprintf("server_timeout_%s_%d", queryID, time.Now().UnixNano()),
					Type:      string(unifiedevents.EventTypeUnifiedCompletion),
					Timestamp: time.Now(),
					Data:      agentEvent,
					SessionID: sessionID,
				}
				api.eventStore.AddEvent(sessionID, serverTimeoutEvent)
				log.Printf("[SERVER DEBUG] Emitted server timeout completion event for query %s", queryID)
				return
			default:
			}
		}
		log.Printf("[STREAMING_LIFECYCLE] StreamWithEvents completed | session=%s chunks=%d duration=%dms", sessionID, chunkCount, time.Since(startTime).Milliseconds())
		log.Printf("[AGENT DEBUG] After streaming loop, streamCtx.Err(): %v", streamCtx.Err())

		// Clean up running agent reference (steer injection no longer possible)
		api.runningAgentsMux.Lock()
		delete(api.runningAgents, sessionID)
		api.runningAgentsMux.Unlock()

		// Final save of conversation history (in case streaming was stopped mid-way)
		// This ensures we capture the final state even if streaming was interrupted.
		// finalHistory is what the agent saw — after a mode change that's
		// [recap_msg, new_user_msg, ai_response, …]. We keep that truncated
		// view in memory so the next turn's recap-replay stays tight, but the
		// on-disk record needs the full conversation; persistedHistory below
		// merges the pre-change snapshot with the new exchange.
		finalHistory := llmAgent.GetHistory()
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = finalHistory
		api.conversationMux.Unlock()
		log.Printf("[CONVERSATION DEBUG] Final save: %d messages to conversation history for session %s", len(finalHistory), sessionID)

		// What we write to disk. Defaults to finalHistory; if mode changed
		// this turn, drop the synthetic recap message (index 0) and append
		// the rest to the pre-change snapshot so the persisted file stays
		// the canonical record of the conversation.
		persistedHistory := finalHistory
		if modeChangedThisTurn && len(preModeChangeSnapshot) > 0 && len(finalHistory) >= 1 {
			merged := make([]llmtypes.MessageContent, 0, len(preModeChangeSnapshot)+len(finalHistory))
			merged = append(merged, preModeChangeSnapshot...)
			merged = append(merged, finalHistory[1:]...) // skip the recap-as-user-message
			persistedHistory = merged
			log.Printf("[CONVERSATION DEBUG] Mode-change merge: persisting %d msgs (snapshot %d + new %d)", len(persistedHistory), len(preModeChangeSnapshot), len(finalHistory)-1)
		}

		// Persist conversation to user's chat_history/ folder (same format as builder/)
		api.persistChatConversation(sessionID, req.AgentMode, currentUserID, persistedHistory)

		// Store resolved workflowPhaseFolder so synthetic turns can persist builder conversations
		if isWorkflowPhase && workflowPhaseFolder != "" {
			api.sessionWorkspaceMu.Lock()
			api.sessionWorkspaceFolders[sessionID] = workflowPhaseFolder
			api.sessionWorkspaceMu.Unlock()
		}

		// Save builder conversation log + token_usage.json for workflow phase sessions.
		// One file per session — overwrites on each follow-up with the full cumulative history.
		// Resolve workspace-docs root so files are visible in the UI.
		if isWorkflowPhase && workflowPhaseFolder != "" && len(persistedHistory) > 0 {
			convData := map[string]interface{}{
				"session_id":           sessionID,
				"phase_id":             workflowPhaseID,
				"conversation_history": persistedHistory,
				"updated_at":           time.Now().Format(time.RFC3339),
			}
			if convJSON, err := json.MarshalIndent(convData, "", "  "); err == nil {
				logPath := workflowBuilderConversationLogPath(workflowPhaseFolder, sessionID, time.Now())
				if err := writeRawFileToWorkspace(context.Background(), logPath, string(convJSON)); err != nil {
					log.Printf("[BUILDER LOG] Failed to write conversation log: %v", err)
				} else {
					log.Printf("[BUILDER LOG] Saved conversation log (%d messages) to %s", len(finalHistory), logPath)
				}
			}

			if underlying := llmAgent.GetUnderlyingAgent(); underlying != nil {
				promptTokens, completionTokens, _, cacheTokens, reasoningTokens, llmCallCount, _,
					inputCost, outputCost, reasoningCost, cacheCost, totalCost, _ := underlying.GetTokenUsageWithPricing()

				fmtM := func(tokens int) string {
					return fmt.Sprintf("%.3fM", float64(tokens)/1_000_000.0)
				}

				phaseKey := workflowPhaseID
				modelUsage := &orchestrator.ModelTokenUsage{
					Provider:         finalProvider,
					InputTokens:      promptTokens,
					OutputTokens:     completionTokens,
					InputTokensM:     fmtM(promptTokens),
					OutputTokensM:    fmtM(completionTokens),
					CacheTokens:      cacheTokens,
					CacheTokensM:     fmtM(cacheTokens),
					ReasoningTokens:  reasoningTokens,
					ReasoningTokensM: fmtM(reasoningTokens),
					LLMCallCount:     llmCallCount,
					InputCost:        inputCost,
					OutputCost:       outputCost,
					ReasoningCost:    reasoningCost,
					CacheCost:        cacheCost,
					TotalCost:        totalCost,
				}

				workflowRoot := workflowPhaseFolder
				legacyTokenFilePath := filepath.Join(workflowRoot, "token_usage.json")
				tokenFilePath := filepath.Join(workflowRoot, "costs", "phase", "token_usage.json")
				var tokenFile orchestrator.PhaseTokenUsageFile
				if existingData, exists, err := readFileFromWorkspace(context.Background(), tokenFilePath); err == nil && exists {
					_ = json.Unmarshal([]byte(existingData), &tokenFile)
				} else if existingData, exists, err := readFileFromWorkspace(context.Background(), legacyTokenFilePath); err == nil && exists {
					_ = json.Unmarshal([]byte(existingData), &tokenFile)
				}
				now := time.Now()
				orchestrator.ApplyModelUsageToPhaseTokenUsageFile(&tokenFile, phaseKey, underlying.ModelID, modelUsage, now)

				if tokenJSON, err := json.MarshalIndent(tokenFile, "", "  "); err == nil {
					if err := writeRawFileToWorkspace(context.Background(), tokenFilePath, string(tokenJSON)); err != nil {
						log.Printf("[BUILDER LOG] Failed to write phase token usage: %v", err)
					} else {
						if err := deleteWorkspaceFile(context.Background(), legacyTokenFilePath); err != nil {
							log.Printf("[BUILDER LOG] Failed to delete legacy token_usage.json: %v", err)
						}
						log.Printf("[BUILDER LOG] Updated %s (phase=%s, $%.4f this turn)", tokenFilePath, phaseKey, totalCost)
					}
				}

				dailyTokenFilePath := filepath.Join(workflowRoot, "costs", "phase", "daily", orchestrator.CostDateKey(now)+".json")
				var dailyTokenFile orchestrator.DailyPhaseTokenUsageFile
				if existingData, exists, err := readFileFromWorkspace(context.Background(), dailyTokenFilePath); err == nil && exists {
					_ = json.Unmarshal([]byte(existingData), &dailyTokenFile)
				}
				dailyTokenFile.Date = orchestrator.CostDateKey(now)
				dailyTokenFile.UpdatedAt = now
				if dailyTokenFile.TokenUsage == nil {
					dailyTokenFile.TokenUsage = &orchestrator.PhaseTokenUsageFile{}
				}
				orchestrator.ApplyModelUsageToPhaseTokenUsageFile(dailyTokenFile.TokenUsage, phaseKey, underlying.ModelID, modelUsage, now)

				if dailyTokenJSON, err := json.MarshalIndent(dailyTokenFile, "", "  "); err == nil {
					if err := writeRawFileToWorkspace(context.Background(), dailyTokenFilePath, string(dailyTokenJSON)); err != nil {
						log.Printf("[BUILDER LOG] Failed to write daily phase token usage: %v", err)
					}
				}
			}
		}

		// Save Claude Code CLI session ID for --resume on next turn
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			if ccSID := underlyingAgent.ClaudeCodeSessionID; ccSID != "" {
				api.claudeCodeSessionIDs[sessionID] = ccSID
				log.Printf("[CLAUDE CODE] Saved session ID %s for session %s", ccSID, sessionID)
			}
		}

		// Save Gemini CLI session ID for --resume on next turn
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			if gSID := underlyingAgent.GeminiSessionID; gSID != "" {
				api.geminiSessionIDs[sessionID] = gSID
				log.Printf("[GEMINI CLI] Saved session ID %s for session %s", gSID, sessionID)
			}
			// Save Gemini CLI project dir ID for per-invocation isolation
			if gDirID := underlyingAgent.GeminiProjectDirID; gDirID != "" {
				api.geminiProjectDirIDs[sessionID] = gDirID
				workspace.SetSessionGeminiProjectDirID(sessionID, gDirID)
				log.Printf("[GEMINI CLI] Saved project dir ID %s for session %s", gDirID, sessionID)
			}
		}

		// Store agent for reuse by synthetic turns (multi-agent chat and workflow phase chat).
		// The stored agent retains all tools, prompts, observers, and conversation history.
		{
			api.sessionAgentsMux.Lock()
			api.sessionAgents[sessionID] = llmAgent
			api.sessionAgentsMux.Unlock()
			log.Printf("[BG AGENT] Stored agent for session %s for synthetic turn reuse", sessionID)
		}

		// Clean up the agent cancel function when streaming is complete
		api.agentCancelMux.Lock()
		delete(api.agentCancelFuncs, sessionID)
		api.agentCancelMux.Unlock()

		// Update active session status to completed
		log.Printf("[COMPLETION] Updating session %s status to completed", sessionID)
		api.updateSessionStatus(sessionID, "completed")

		// End conversation trace
		tracer.EndTrace(traceID, map[string]interface{}{
			"status": "completed",
		})

		// Note: Completion events are emitted by the underlying agent, no need for server-level events

		log.Printf("[AGENT DEBUG] Query %s completed successfully", queryID)
	}()
}

func (api *StreamingAPI) verifySessionAccess(r *http.Request, sessionID string) error {
	currentUserID := GetUserIDFromContext(r.Context())
	api.activeSessionsMux.RLock()
	activeSession, exists := api.activeSessions[sessionID]
	api.activeSessionsMux.RUnlock()
	if !exists || (currentUserID != "" && activeSession.UserID != "" && activeSession.UserID != currentUserID) {
		return fmt.Errorf("session not found or access denied")
	}

	log.Printf("[SESSION STOP] Workflow session %s not in DB, verified via activeSessions (mode=%s)", sessionID, activeSession.AgentMode)
	return nil
}

// handleCancelCurrentTurn cancels only the currently running LLM turn for a session.
// It must not mark the session stopped or tear down workshop/background state.
func (api *StreamingAPI) handleCancelCurrentTurn(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if err := api.verifySessionAccess(r, sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	api.agentCancelMux.Lock()
	cancelFunc, exists := api.agentCancelFuncs[sessionID]
	if exists {
		cancelFunc()
		delete(api.agentCancelFuncs, sessionID)
		log.Printf("[SESSION DEBUG] Canceled current LLM turn for session %s", sessionID)
	}
	api.agentCancelMux.Unlock()

	if !exists {
		log.Printf("[SESSION DEBUG] No active LLM turn to cancel for session %s", sessionID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// Add endpoint to stop/clear a session
func (api *StreamingAPI) handleStopSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if err := api.verifySessionAccess(r, sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Mark session as stopped FIRST, before any cancellation, so that in-flight
	// goroutines that race with this stop handler will see the flag and bail out
	// instead of re-creating workshop sessions or spawning new CLI processes.
	// See stoppedSessions field comment for the full race condition description.
	api.markSessionStopped(sessionID)

	// Cancel agent execution context if it exists
	api.agentCancelMux.Lock()
	if cancelFunc, exists := api.agentCancelFuncs[sessionID]; exists {
		cancelFunc() // Cancel the agent execution
		delete(api.agentCancelFuncs, sessionID)
		log.Printf("[SESSION DEBUG] Canceled agent execution context for session %s", sessionID)
	}
	api.agentCancelMux.Unlock()

	// Update active session status to stopped
	api.updateSessionStatus(sessionID, "stopped")
	api.setSessionBusy(sessionID, false)
	api.setSyntheticTurn(sessionID, false)

	// Cancel background agents if explicitly requested (e.g. user pressed the stop button).
	// When called before sending a new message, cancelAgents is NOT set so agents survive
	// across turns and synthetic turns can still fire when they complete.
	if r.URL.Query().Get("cancelAgents") == "true" {
		api.bgAgentRegistry.CancelAll(sessionID)
		log.Printf("[SESSION DEBUG] Canceled all background agents for session %s", sessionID)
	}

	// Prevent stopped sessions from being revived by queued background completions or
	// synthetic auto-notification turns that reuse the stored agent after stop.
	api.pendingMu.Lock()
	delete(api.pendingCompletions, sessionID)
	api.pendingMu.Unlock()

	api.lastQueryMu.Lock()
	delete(api.lastQueryRequests, sessionID)
	api.lastQueryMu.Unlock()

	api.sessionWorkspaceMu.Lock()
	delete(api.sessionWorkspaceFolders, sessionID)
	api.sessionWorkspaceMu.Unlock()

	api.sessionAgentsMux.Lock()
	delete(api.sessionAgents, sessionID)
	api.sessionAgentsMux.Unlock()

	api.completionLoopStartedMu.Lock()
	delete(api.completionLoopStarted, sessionID)
	api.completionLoopStartedMu.Unlock()

	api.bgAgentRegistry.Cleanup(sessionID)
	log.Printf("[SESSION DEBUG] Cleared synthetic-turn state for stopped session %s", sessionID)

	// Close workshop chat sessions for this session — cancels all running step executions.
	// Workshop sessions use context.Background() so they survive agent context cancellation above;
	// we must explicitly call Close() to cancel their step goroutines.
	//
	// Close() → cancelFunc() cascades to all execCtx (step goroutines) → kills Codex CLI
	// processes via exec.CommandContext. It also calls CloseWorkshopGroupSessions() which
	// closes MCP connections for group sessions (session-group-*) and isolated sub-sessions.
	//
	// IMPORTANT: The markSessionStopped() call above prevents in-flight goroutines from
	// re-creating the workshop after we close it here. Without that guard, a racing
	// goroutine could call NewWorkshopChatSession() with a fresh context.Background(),
	// creating orphaned CLI processes that are never canceled. See stoppedSessions comment.
	//
	// Historically we keyed this map by sessionID / "eval-"+sessionID, but some workflow
	// execution paths can drift from those exact keys. So first try direct keys, then scan
	// for any workshop session whose owning mainSessionID matches this session.
	closedWorkshopKeys := map[string]struct{}{}
	workshopKeys := []string{sessionID, "eval-" + sessionID}
	for _, wsKey := range workshopKeys {
		if cached, ok := api.workshopChatSessions.Load(wsKey); ok {
			if ws, ok := cached.(interface{ Close() }); ok {
				ws.Close()
				log.Printf("[SESSION DEBUG] Closed workshop session %q (all step executions canceled)", wsKey)
			}
			api.workshopChatSessions.Delete(wsKey)
			closedWorkshopKeys[wsKey] = struct{}{}
		}
	}
	api.workshopChatSessions.Range(func(key, value interface{}) bool {
		wsKey, ok := key.(string)
		if !ok {
			return true
		}
		if _, alreadyClosed := closedWorkshopKeys[wsKey]; alreadyClosed {
			return true
		}
		ws, ok := value.(interface {
			Close()
			MainSessionID() string
		})
		if !ok || ws.MainSessionID() != sessionID {
			return true
		}
		ws.Close()
		api.workshopChatSessions.Delete(wsKey)
		log.Printf("[SESSION DEBUG] Closed workshop session %q via mainSessionID match for session %s", wsKey, sessionID)
		return true
	})

	// Cancel all workflow orchestrator contexts for this session
	// Since we now use queryID as the key, we need to look up all queryIDs for this session
	api.sessionQueryIDMux.Lock()
	queryIDs := api.sessionQueryIDs[sessionID]
	delete(api.sessionQueryIDs, sessionID) // Clear the mapping
	api.sessionQueryIDMux.Unlock()

	if len(queryIDs) > 0 {
		// Cancel all background agents BEFORE canceling workflow contexts.
		// When workflow contexts are canceled, sub-agent goroutines will eventually
		// fail with "context canceled" and call OnExecutionComplete. Without marking
		// them as canceled first, they'd fire stale AUTO-NOTIFICATION synthetic turns.
		api.bgAgentRegistry.CancelAll(sessionID)
		log.Printf("[SESSION DEBUG] Canceled all background agents for session %s (workflow stop)", sessionID)

		api.workflowOrchestratorContextMux.Lock()
		for _, qid := range queryIDs {
			if cancelFunc, exists := api.workflowOrchestratorContexts[qid]; exists {
				cancelFunc() // Cancel this workflow execution
				delete(api.workflowOrchestratorContexts, qid)
				log.Printf("[SESSION DEBUG] Canceled workflow execution %s for session %s", qid, sessionID)
			}
		}
		api.workflowOrchestratorContextMux.Unlock()

		// Remove from active executions registry
		api.activeWorkflowExecutionsMux.Lock()
		for _, qid := range queryIDs {
			delete(api.activeWorkflowExecutions, qid)
		}
		api.activeWorkflowExecutionsMux.Unlock()
		api.cancelTrackedExecutionsForSession(sessionID)
		log.Printf("[SESSION DEBUG] Canceled %d workflow execution(s) for session %s", len(queryIDs), sessionID)
	}

	// Clear workflow objective
	api.workflowObjectiveMux.Lock()
	if _, exists := api.workflowObjectives[sessionID]; exists {
		delete(api.workflowObjectives, sessionID)
		log.Printf("[SESSION DEBUG] Cleared workflow objective for session %s", sessionID)
	}
	api.workflowObjectiveMux.Unlock()

	// Close all MCP sessions (browsers, etc.) associated with this HTTP session immediately.
	// This is safe to call even if the defers in the workflow goroutines haven't fired yet —
	// CloseSession is idempotent, so those defers will be no-ops when they eventually run.
	log.Printf("[SESSION DEBUG] Closing MCP sessions for stopped session %s", sessionID)
	mcpagent.CloseHTTPSession(sessionID)

	// Kill headless browser processes for this session
	api.cleanupBrowserSessions(sessionID)

	// Note: Conversation history and orchestrator state are preserved to allow resuming the conversation
	// Use /api/session/clear if you want to clear conversation history

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Session stopped (conversation history and orchestrator state preserved)"))
}

// handleGetBrowserSessions returns the tracked browser sessions with their owning chat session IDs.
func (api *StreamingAPI) handleGetBrowserSessions(w http.ResponseWriter, r *http.Request) {
	tracker := browser.GetSessionTracker()
	sessions := tracker.ActiveSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// cleanupBrowserSessions closes all headless browser processes for a session.
// Must be called whenever a session ends (stop, clear, workflow completion).
func (api *StreamingAPI) cleanupBrowserSessions(sessionID string) {
	tracker := browser.GetSessionTracker()
	if tracker.CountForChat(sessionID) == 0 {
		return
	}
	workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
	if workspaceAPIURL == "" {
		workspaceAPIURL = "http://127.0.0.1:8081"
	}
	client := browser.NewClient(workspaceAPIURL)
	tracker.CloseAllForChat(sessionID, client)
}

// Add endpoint to clear conversation history for a session
func (api *StreamingAPI) handleClearSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Verify session ownership via the in-memory active sessions map.
	if err := api.verifySessionAccess(r, sessionID); err != nil {
		http.Error(w, "Session not found or access denied", http.StatusNotFound)
		return
	}

	// Clear conversation history
	api.conversationMux.Lock()
	if _, exists := api.conversationHistory[sessionID]; exists {
		delete(api.conversationHistory, sessionID)
		log.Printf("[SESSION DEBUG] Cleared conversation history for session %s", sessionID)
	}
	api.conversationMux.Unlock()

	// Clear orchestrator state (removed - now stateless)

	// Clear orchestrator instance (legacy removed)

	// Clear workflow objective
	api.workflowObjectiveMux.Lock()
	if _, exists := api.workflowObjectives[sessionID]; exists {
		delete(api.workflowObjectives, sessionID)
		log.Printf("[SESSION DEBUG] Cleared workflow objective for session %s", sessionID)
	}
	api.workflowObjectiveMux.Unlock()

	// Clear Claude Code CLI session ID
	delete(api.claudeCodeSessionIDs, sessionID)

	// Clear Gemini CLI session ID and project dir ID
	delete(api.geminiSessionIDs, sessionID)
	delete(api.geminiProjectDirIDs, sessionID)

	// Clear last-seen workshop mode (used to detect mode toggles for recap injection)
	delete(api.lastWorkshopModeBySession, sessionID)

	// Kill headless browser processes for this session
	api.cleanupBrowserSessions(sessionID)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Session cleared (conversation history and orchestrator state removed)"))
}

// State management functions removed - orchestrator is now stateless

// createServerLogger creates a logger instance for the server
// This logger writes to stdout only to avoid duplication with shell redirection
func createServerLogger() loggerv2.Logger {
	// Force stdout logging by passing empty log file and enableStdout=true
	// This prevents the application from writing to the file directly,
	// allowing the shell script's redirection to handle file logging without duplicates.
	logFile := ""

	// Check for log level from environment variable
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	serverLogger, err := logger.CreateLogger(logFile, logLevel, "text", true)
	if err != nil {
		log.Fatalf("Failed to create server logger: %v", err)
	}
	return serverLogger
}

// createLLMLogger creates a separate logger instance for LLM operations
// This logger writes to logs/llm_debug.log to separate LLM logs from server logs
func createLLMLogger() loggerv2.Logger {
	llmLogger, err := logger.CreateLogger("logs/llm_debug.log", "debug", "text", false)
	if err != nil {
		log.Fatalf("Failed to create LLM logger: %v", err)
	}
	return llmLogger
}

// --- ACTIVE SESSION MANAGEMENT ---

// trackActiveSession tracks a new active session
func (api *StreamingAPI) trackActiveSession(sessionID, agentMode, query, userID, botPlatform, triggeredBy string) {
	if api.eventStore != nil {
		api.eventStore.SetSessionOwner(sessionID, userID)
	}

	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	if existing := api.activeSessions[sessionID]; existing != nil {
		if botPlatform == "" {
			botPlatform = existing.BotPlatform
		}
		if triggeredBy == "" {
			triggeredBy = existing.TriggeredBy
		}
	}

	api.activeSessions[sessionID] = &ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    agentMode,
		Status:       "running",
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
		Query:        query,
		UserID:       userID,
		BotPlatform:  botPlatform,
		TriggeredBy:  triggeredBy,
	}

	logfWithContext(
		newServerLogContext("", "", agentMode, userID, "", sessionID),
		"[ACTIVE_SESSION] Tracked active session: %s (mode: %s, user: %s)",
		sessionID,
		agentMode,
		userID,
	)
}

// updateSessionActivity updates the LastActivity timestamp for a session when events are added
func (api *StreamingAPI) updateSessionActivity(sessionID string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	if session, exists := api.activeSessions[sessionID]; exists {
		session.LastActivity = time.Now()
		// Don't log every activity update to avoid log spam
	}
}

// updateSessionStatus updates the status of an active session in memory.
func (api *StreamingAPI) updateSessionStatus(sessionID, status string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()
	if session, exists := api.activeSessions[sessionID]; exists {
		session.Status = status
		session.LastActivity = time.Now()
		log.Printf("[ACTIVE_SESSION] Updated session %s status to: %s", sessionID, status)
	}
}

// handleDismissSession marks a session as dismissed so it won't be auto-restored on page refresh.
func (api *StreamingAPI) handleDismissSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	api.activeSessionsMux.Lock()
	if session, exists := api.activeSessions[sessionID]; exists {
		session.Status = "dismissed"
		session.LastActivity = time.Now()
	}
	api.activeSessionsMux.Unlock()

	log.Printf("[ACTIVE_SESSION] Dismissed session %s", sessionID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "dismissed",
		"session": sessionID,
	})
}

// sessionEventEmitter implements virtualtools.SessionEventEmitter to emit
// blocking_human_feedback events for human-input tools.
type sessionEventEmitter struct {
	eventStore *events.EventStore
	sessionID  string
}

func (e *sessionEventEmitter) EmitBlockingHumanFeedback(requestID, question, contextText string, yesNoOnly bool, yesLabel, noLabel string, options ...string) {
	now := time.Now()
	eventData := &orchEvents.BlockingHumanFeedbackEvent{
		BaseEventData: unifiedevents.BaseEventData{
			Timestamp: now,
		},
		Question:      question,
		AllowFeedback: !yesNoOnly && len(options) == 0, // Allow text input when not yes/no and no options
		Context:       contextText,
		SessionID:     e.sessionID,
		RequestID:     requestID,
		YesNoOnly:     yesNoOnly,
		YesLabel:      yesLabel,
		NoLabel:       noLabel,
		Options:       options,
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_plan_approval_%d", e.sessionID, now.UnixNano()),
		Type:      "blocking_human_feedback",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      orchEvents.BlockingHumanFeedback,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "delegation",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	log.Printf("[PLAN APPROVAL] Emitted blocking_human_feedback event for plan approval (request_id: %s, session: %s)", requestID, e.sessionID)
}

// executeDelegatedTask executes a delegated task via a sub-agent.
// onCreated is an optional callback invoked after the sub-agent wrapper is created
// but before Invoke — used by background agents to attach a history func.
func (api *StreamingAPI) executeDelegatedTask(ctx context.Context, parentReq QueryRequest, sessionID string, instruction string, onCreated ...func(wrapper *agent.LLMAgentWrapper)) (string, error) {
	log.Printf("[DELEGATION] Creating sub-agent for delegated task in session %s", sessionID)

	// Check delegation depth from context
	currentDepth := 0
	if depth, ok := ctx.Value(virtualtools.DelegationDepthKey).(int); ok {
		currentDepth = depth
	}

	if currentDepth >= virtualtools.MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached", virtualtools.MaxDelegationDepth)
	}

	// Generate a unique delegation ID for tracking
	delegationID := fmt.Sprintf("delegation-%d-%d", currentDepth, time.Now().UnixNano())

	// Build sub-agent config from parent request
	// Get provider and model from parent request
	provider := llm.Provider(parentReq.Provider)
	if provider == "" {
		provider = llm.Provider("anthropic")
	}
	modelID := parentReq.ModelID
	if modelID == "" {
		modelID = "claude-sonnet-4-20250514"
	}

	// Load sub-agent template if specified
	var loadedTemplate *subagents.SubAgent
	agentTemplateName, _ := ctx.Value(virtualtools.AgentTemplateKey).(string)
	if agentTemplateName != "" {
		workspaceAPIURL := getWorkspaceAPIURL()
		sa, err := subagents.GetSubAgent(workspaceAPIURL, agentTemplateName)
		if err != nil {
			log.Printf("[DELEGATION] Warning: Failed to load sub-agent template %s: %v", agentTemplateName, err)
		} else {
			loadedTemplate = sa
			log.Printf("[DELEGATION] Loaded sub-agent template: %s (%s)", sa.Frontmatter.Name, agentTemplateName)
		}
	}

	// Resolve reasoning level tier to specific provider/model if configured
	reasoningLevel, _ := ctx.Value(virtualtools.ReasoningLevelKey).(string)
	// Apply template defaults if not explicitly set
	if reasoningLevel == "" && loadedTemplate != nil && loadedTemplate.Frontmatter.DefaultReasoningLevel != "" {
		reasoningLevel = loadedTemplate.Frontmatter.DefaultReasoningLevel
		log.Printf("[DELEGATION] Using template default reasoning_level: %s", reasoningLevel)
	}
	var tierFallbacks []agent.FallbackModel
	if reasoningLevel != "" {
		// Load fresh from workspace file at delegation time so LLM-written tier changes take effect immediately
		tierConfig := LoadAndResolveTierConfig(ctx, parentReq.DelegationTierConfig)
		if tierConfig != nil {
			var tierModel *virtualtools.TierModel
			switch reasoningLevel {
			case "high":
				tierModel = tierConfig.High
			case "medium":
				tierModel = tierConfig.Medium
			case "low":
				tierModel = tierConfig.Low
			default:
				// Custom tier lookup
				if tierConfig.Custom != nil {
					if ct, ok := tierConfig.Custom[reasoningLevel]; ok {
						tierModel = &virtualtools.TierModel{Provider: ct.Provider, ModelID: ct.ModelID}
					}
				}
			}
			if tierModel != nil && tierModel.Provider != "" && tierModel.ModelID != "" {
				provider = llm.Provider(tierModel.Provider)
				modelID = tierModel.ModelID
				tierFallbacks = convertTierFallbacksToAgentFallbacks(tierModel.Fallbacks, tierModel.Provider)
				log.Printf("[DELEGATION] Using tier %s model: %s/%s", reasoningLevel, tierModel.Provider, tierModel.ModelID)
			}
		}
	}

	// Build server name — use delegation-specific servers if provided, otherwise all parent servers
	var serverName string
	var serversList []string
	if delegationServers, ok := ctx.Value(virtualtools.DelegationServersKey).([]string); ok && len(delegationServers) > 0 {
		serverName = strings.Join(delegationServers, ",")
		serversList = delegationServers
		log.Printf("[DELEGATION] Using sub-agent specific servers: %s", serverName)
	} else if len(parentReq.EnabledServers) > 0 {
		serverName = strings.Join(parentReq.EnabledServers, ",")
		serversList = parentReq.EnabledServers
	} else if len(parentReq.Servers) > 0 {
		serverName = strings.Join(parentReq.Servers, ",")
		serversList = parentReq.Servers
	}

	// Sub-agents always run in code_execution mode (Python harness calling MCP tools via HTTP API).
	useCodeExec := true

	// Extract background agent ID if this delegation was spawned by a background agent
	backgroundAgentID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)

	// Emit delegation_start event (after model and server resolution so we can include all info)
	api.emitDelegationStartEvent(sessionID, delegationID, currentDepth, instruction, reasoningLevel, modelID, serversList, backgroundAgentID, agentTemplateName)

	// Load merged API keys (env + workspace)
	apiKeys := MergedProviderAPIKeys(ctx)

	// Get user ID from context for per-user OAuth token isolation
	subAgentUserID := ""
	if userID, ok := ctx.Value(common.UserIDKey).(string); ok {
		subAgentUserID = userID
	}
	log.Printf("[USER_ID_DEBUGGING] Sub-agent: subAgentUserID=%q (from parent context UserIDKey)", subAgentUserID)

	// Determine sub-agent session ID: isolated when share_browser=false, shared otherwise
	subAgentSessionID := sessionID
	if sb, ok := ctx.Value(virtualtools.ShareBrowserKey).(bool); ok && !sb {
		subAgentSessionID = fmt.Sprintf("%s-isolated-%d", sessionID, time.Now().UnixNano())
		log.Printf("[DELEGATION] Browser isolation: sub-agent gets new session ID %s (parent: %s)", subAgentSessionID, sessionID)
	}

	// Create sub-agent config based on parent request
	subAgentConfig := agent.LLMAgentConfig{
		Name:       fmt.Sprintf("sub-agent-depth-%d", currentDepth),
		ServerName: serverName,
		ConfigPath: api.mcpConfigPath,
		Provider:   provider,
		ModelID:    modelID,
		Temperature: func() float64 {
			if parentReq.Temperature > 0 {
				return parentReq.Temperature
			}
			return 0.7
		}(),
		MaxTurns: func() int {
			if parentReq.MaxTurns != 0 {
				return parentReq.MaxTurns
			}
			return 500
		}(),
		ToolChoice:         "", // Empty — let the library decide; Azure/OpenAI reject tool_choice when no tools are present
		StreamingChunkSize: 1,
		// No Timeout set — sub-agent lifetime is controlled by the parent context.
		ToolTimeout: func() time.Duration {
			if envVal := os.Getenv("TOOL_EXECUTION_TIMEOUT"); envVal != "" {
				if timeout, err := time.ParseDuration(envVal); err == nil && timeout > 0 {
					return timeout
				}
			}
			return 5 * time.Minute
		}(),
		// Sub-agent mode uses the resolved values (from delegate call, template default, or auto-enable).
		UseCodeExecutionMode: useCodeExec,
		APIKeys:              apiKeys,
		Fallbacks:            tierFallbacks,
		SessionID:            subAgentSessionID, // Reuse parent session's MCP connections via registry, unless browser isolation requested
		UserID:               subAgentUserID,    // Per-user OAuth token isolation
		// Context offloading: inherit from environment
		LargeOutputThreshold: func() int {
			if envVal := os.Getenv("LARGE_OUTPUT_THRESHOLD"); envVal != "" {
				if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
					return threshold
				}
			}
			return 0
		}(),
		// Context summarization: inherit from parent request > env > defaults
		EnableContextSummarization: func() bool {
			if parentReq.EnableContextSummarization != nil {
				return *parentReq.EnableContextSummarization
			}
			if envVal := os.Getenv("ENABLE_CONTEXT_SUMMARIZATION"); envVal == "false" {
				return false
			}
			return true
		}(),
		SummarizeOnTokenThreshold: func() bool {
			if parentReq.SummarizeOnTokenThreshold != nil {
				return *parentReq.SummarizeOnTokenThreshold
			}
			if envVal := os.Getenv("SUMMARIZE_ON_TOKEN_THRESHOLD"); envVal == "false" {
				return false
			}
			return true
		}(),
		TokenThresholdPercent: func() float64 {
			if parentReq.TokenThresholdPercent > 0 {
				return parentReq.TokenThresholdPercent
			}
			if envVal := os.Getenv("TOKEN_THRESHOLD_PERCENT"); envVal != "" {
				if threshold, err := strconv.ParseFloat(envVal, 64); err == nil && threshold > 0 && threshold <= 1.0 {
					return threshold
				}
			}
			return 0.8
		}(),
		SummarizeOnFixedTokenThreshold: func() bool {
			if parentReq.SummarizeOnFixedTokenThreshold != nil {
				return *parentReq.SummarizeOnFixedTokenThreshold
			}
			if envVal := os.Getenv("SUMMARIZE_ON_FIXED_TOKEN_THRESHOLD"); envVal == "false" {
				return false
			}
			return true
		}(),
		FixedTokenThreshold: func() int {
			if parentReq.FixedTokenThreshold > 0 {
				return parentReq.FixedTokenThreshold
			}
			if envVal := os.Getenv("FIXED_TOKEN_THRESHOLD"); envVal != "" {
				if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
					return threshold
				}
			}
			return 200000
		}(),
		SummaryKeepLastMessages: func() int {
			if parentReq.SummaryKeepLastMessages > 0 {
				return parentReq.SummaryKeepLastMessages
			}
			if envVal := os.Getenv("SUMMARY_KEEP_LAST_MESSAGES"); envVal != "" {
				if keepLast, err := strconv.Atoi(envVal); err == nil && keepLast > 0 {
					return keepLast
				}
			}
			return 4
		}(),
		// Context editing: inherit from parent request > env > defaults
		EnableContextEditing: func() bool {
			if parentReq.EnableContextEditing != nil {
				return *parentReq.EnableContextEditing
			}
			if envVal := os.Getenv("ENABLE_CONTEXT_EDITING"); envVal == "true" {
				return true
			}
			return false
		}(),
		ContextEditingThreshold: func() int {
			if parentReq.ContextEditingThreshold > 0 {
				return parentReq.ContextEditingThreshold
			}
			if envVal := os.Getenv("CONTEXT_EDITING_THRESHOLD"); envVal != "" {
				if threshold, err := strconv.Atoi(envVal); err == nil && threshold > 0 {
					return threshold
				}
			}
			return 0
		}(),
		ContextEditingTurnThreshold: func() int {
			if parentReq.ContextEditingTurnThreshold > 0 {
				return parentReq.ContextEditingTurnThreshold
			}
			if envVal := os.Getenv("CONTEXT_EDITING_TURN_THRESHOLD"); envVal != "" {
				if turnThreshold, err := strconv.Atoi(envVal); err == nil && turnThreshold > 0 {
					return turnThreshold
				}
			}
			return 0
		}(),
		// Parallel tool execution: enabled by default, can be disabled via ENABLE_PARALLEL_TOOL_EXECUTION=false
		EnableParallelToolExecution: func() bool {
			if envVal := os.Getenv("ENABLE_PARALLEL_TOOL_EXECUTION"); envVal == "false" {
				return false
			}
			return true
		}(),
	}

	// Create sub-agent using the wrapper (same as parent agent creation)
	subAgent, err := agent.NewLLMAgentWrapper(ctx, subAgentConfig, nil, api.logger)
	if err != nil {
		api.emitDelegationEndEvent(sessionID, delegationID, currentDepth, "", err.Error(), nil)
		return "", fmt.Errorf("failed to create sub-agent: %w", err)
	}

	// Resolve conditional folder-guard grants for the sub-agent once.
	// Used by both nested scopes below (prompt assembly + workspace tool folder guard).
	subResolvedGrants := resolveConditionalGrants(parentReq)

	// Add event observers to sub-agent so its events appear in the UI and
	// its token usage lands in the global cost ledger.
	if underlyingAgent := subAgent.GetUnderlyingAgent(); underlyingAgent != nil {
		subAgentObserver := events.NewDelegationEventObserver(api.eventStore, sessionID, currentDepth, delegationID, api.logger)
		if toolCb, ok := ctx.Value(virtualtools.ToolEventCallbackKey).(events.ToolEventCallback); ok && toolCb != nil {
			subAgentObserver.OnToolEvent = toolCb
		}
		underlyingAgent.AddEventListener(subAgentObserver)
		parentUserID, _ := ctx.Value(common.UserIDKey).(string)
		underlyingAgent.AddEventListener(newCostObserver(api.costLedger, sessionID, parentUserID, parentReq.AgentMode))
		log.Printf("[DELEGATION] Added event observers for sub-agent at depth %d", currentDepth)

		// Add skill instructions to sub-agent system prompt (mirrors parent agent setup)
		if len(parentReq.SelectedSkills) > 0 {
			skillPrompt := buildSkillPrompt(parentReq.SelectedSkills, getWorkspaceAPIURL(), fsutil.WorkspaceShellRoot(), false)
			if skillPrompt != "" {
				underlyingAgent.AppendSystemPrompt(skillPrompt)
				log.Printf("[DELEGATION] Added skill instructions to sub-agent (%d skills)", len(parentReq.SelectedSkills))
			}
		}

		// Append prompt sections contributed by active conditional grants
		// (resolved above in subResolvedGrants before this block).
		for _, section := range subResolvedGrants.PromptSections {
			underlyingAgent.AppendSystemPrompt(section)
		}
		if len(subResolvedGrants.PromptSections) > 0 {
			log.Printf("[DELEGATION] Appended %d grant prompt section(s) to sub-agent: %v", len(subResolvedGrants.PromptSections), subResolvedGrants.AppliedNames)
		}

		// Inject sub-agent template instructions into system prompt
		if loadedTemplate != nil {
			templatePrompt := fmt.Sprintf("\n## Sub-Agent Role: %s\n\n%s\n",
				loadedTemplate.Frontmatter.Name, loadedTemplate.Content)
			underlyingAgent.AppendSystemPrompt(templatePrompt)
			log.Printf("[DELEGATION] Injected sub-agent template instructions: %s", loadedTemplate.Frontmatter.Name)
		}

		// Merge global secrets with parent's decrypted secrets — inject names into prompt (values are in env vars)
		allDelegationSecrets := mergeGlobalSecrets(parentReq.DecryptedSecrets, parentReq.SelectedGlobalSecrets)
		if len(allDelegationSecrets) > 0 {
			var secretNames []string
			for _, s := range allDelegationSecrets {
				secretNames = append(secretNames, "- `SECRET_"+s.Name+"` → accessible as `os.environ[\"SECRET_"+s.Name+"\"]` in Python or `$SECRET_"+s.Name+"` in bash")
			}
			secretPrompt := "\n## Secrets\n\nThe following secrets are available as environment variables in execute_shell_command. Do NOT ask the user for these values — read them from the environment.\n\n" + strings.Join(secretNames, "\n")
			underlyingAgent.AppendSystemPrompt(secretPrompt)
			log.Printf("[DELEGATION] Injected %d secret names (not values) into sub-agent system prompt", len(allDelegationSecrets))
		}

		// Give sub-agents the workspace folder structure so they know where to
		// read/write files. Sub-agents are actual file workers that need this orientation.
		// Use the same per-user Chats folder as the parent session.
		subAgentChatsFolder := perUserChatsFolderFor(subAgentUserID)
		subAgentMemoryFolder := perUserMemoryFolderFor(subAgentUserID)
		subShellRoot := fsutil.WorkspaceShellRoot()
		underlyingAgent.AppendSystemPrompt(GetWorkspaceMap(subShellRoot, subAgentChatsFolder, subAgentMemoryFolder))
		underlyingAgent.AppendSystemPrompt(GetWorkspaceReference(subShellRoot, subAgentChatsFolder, subAgentMemoryFolder))
		log.Printf("[DELEGATION] Added workspace instructions to sub-agent (chats=%s)", subAgentChatsFolder)

		// Give sub-agents access to memory tools so they can persist key discoveries
		// across tasks (reads from Chats/memories/ by default).
		api.activeSessionsMux.RLock()
		subAgentMemFolder := ""
		if sess, ok := api.activeSessions[sessionID]; ok {
			subAgentMemFolder = sess.MemoryFolder
		}
		api.activeSessionsMux.RUnlock()
		underlyingAgent.AppendSystemPrompt(virtualtools.GetMemoryInstructions(subAgentMemFolder))
		log.Printf("[DELEGATION] Added memory instructions to sub-agent")

		// [BROWSER] Add browser instructions using standardized builder (same as parent chat agent).
		// Sub-agents need their own transformer registration because each Agent instance has
		// its own toolArgTransformers map — the parent's transformer doesn't propagate.
		subBrowserCfg := buildChatBrowserConfig(parentReq)
		if subBrowserPrompt := browserinstructions.BuildBrowserInstructions(subBrowserCfg); subBrowserPrompt != "" {
			underlyingAgent.AppendSystemPrompt(subBrowserPrompt)
			log.Printf("[BROWSER] Added browser instructions to sub-agent (playwright=%v, agent-browser=%v, cdp=%v)",
				subBrowserCfg.HasPlaywright, subBrowserCfg.HasAgentBrowser, subBrowserCfg.CdpPort > 0)
		}

		// [GWS] Add GWS quick-start instructions to sub-agent (same as parent)
		if parentReq.EnableGWSAccess != nil && *parentReq.EnableGWSAccess {
			underlyingAgent.AppendSystemPrompt(browserinstructions.GetGWSQuickStartInstructions())
			log.Printf("[GWS] Added GWS quick-start instructions to sub-agent")
		}

		// Register file path transformer for browser file uploads on sub-agent
		hasBrowserAccess := parentReq.EnableBrowserAccess != nil && *parentReq.EnableBrowserAccess
		hasPlaywright := false
		for _, s := range parentReq.EnabledServers {
			if s == "playwright" {
				hasPlaywright = true
			}
		}
		if hasBrowserAccess || hasPlaywright {
			wsAbsPath := fsutil.WorkspaceShellRoot()
			underlyingAgent.SetToolArgTransformer("browser_file_upload", func(args map[string]interface{}) {
				paths, ok := args["paths"].([]interface{})
				if !ok || len(paths) == 0 {
					log.Printf("[BROWSER_UPLOAD] Sub-agent: no paths in args, skipping transform")
					return
				}
				for i, p := range paths {
					pathStr, ok := p.(string)
					if !ok || pathStr == "" || filepath.IsAbs(pathStr) {
						continue
					}
					resolved := filepath.Join(wsAbsPath, pathStr)
					log.Printf("[BROWSER_UPLOAD] Sub-agent resolved path[%d]: %q -> %q", i, pathStr, resolved)
					paths[i] = resolved
				}
			})
			log.Printf("[BROWSER_UPLOAD] Registered sub-agent browser_file_upload transformer, workspace=%s", wsAbsPath)
		}

		// Browser isolation: when share_browser=false, tell the sub-agent to use a unique
		// session name with the agent_browser tool to avoid sharing browser state.
		if sb, ok := ctx.Value(virtualtools.ShareBrowserKey).(bool); ok && !sb {
			underlyingAgent.AppendSystemPrompt(fmt.Sprintf("## Browser Isolation\nYou have an isolated browser session. When using the agent_browser tool, use a unique session name (e.g., \"isolated-%d\") instead of \"default\" to avoid sharing browser state with other agents.", time.Now().UnixNano()))
			log.Printf("[DELEGATION] Added browser isolation guidance to sub-agent system prompt")
		}
	}

	// Register workspace tools for sub-agent
	if underlyingAgent := subAgent.GetUnderlyingAgent(); underlyingAgent != nil {
		// Sub-agents get the normal LLM-visible workspace tool set (advanced + image + video + audio + music).
		workspaceTools := append(virtualtools.CreateWorkspaceAdvancedTools(), virtualtools.CreateWorkspaceImageTools()...)
		workspaceTools = append(workspaceTools, virtualtools.CreateWorkspaceVideoTools()...)
		workspaceTools = append(workspaceTools, virtualtools.CreateWorkspaceAudioTools()...)
		workspaceTools = append(workspaceTools, virtualtools.CreateWorkspaceMusicTools()...)
		workspaceExecutors, subAgentEnv := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(subAgentUserID, sessionID)
		virtualtools.MergeImageToolExecutors(virtualtools.ImageGenExecutorConfig{
			WorkspaceAPIURL: getWorkspaceAPIURL(),
			UserID:          subAgentUserID,
		}, workspaceExecutors, nil)
		virtualtools.MergeVideoToolExecutors(virtualtools.VideoGenExecutorConfig{
			WorkspaceAPIURL: getWorkspaceAPIURL(),
			UserID:          subAgentUserID,
		}, workspaceExecutors, nil)
		virtualtools.MergeAudioToolExecutors(virtualtools.AudioGenExecutorConfig{
			WorkspaceAPIURL: getWorkspaceAPIURL(),
			UserID:          subAgentUserID,
		}, workspaceExecutors, nil)
		virtualtools.MergeMusicToolExecutors(virtualtools.AudioGenExecutorConfig{
			WorkspaceAPIURL: getWorkspaceAPIURL(),
			UserID:          subAgentUserID,
		}, workspaceExecutors, nil)
		log.Printf("[USER_ID_DEBUGGING] Sub-agent workspace executors: created with explicit userID=%q sessionID=%q", subAgentUserID, sessionID)
		// Inject secrets as environment variables for sub-agent shell execution (SECRET_ prefix for whitelist)
		delegationSecrets := mergeGlobalSecrets(parentReq.DecryptedSecrets, parentReq.SelectedGlobalSecrets)
		if subAgentEnv != nil && len(delegationSecrets) > 0 {
			for _, s := range delegationSecrets {
				subAgentEnv["SECRET_"+s.Name] = s.Value
			}
			log.Printf("[SECRETS] Injected %d secrets as env vars for sub-agent shell execution", len(delegationSecrets))
		}
		// Inject LLM config fallback for read_image HTTP calls (e.g., from claude CLI subprocess)
		if underlying := subAgent.GetUnderlyingAgent(); underlying != nil {
			virtualtools.SetReadImageFallbackLLMConfig(workspaceExecutors, underlying.GetLLMModelConfig())
		}
		_, _, toolCategories := createCustomTools(false, subAgentUserID, sessionID)

		// Conditional grants already resolved above into subResolvedGrants.
		// Merge parent @context paths and #workflow references into delegated folder-guard access.
		// @context paths get write access; #workflow paths get read-only access.
		fileContextWriteFolders, workflowReadOnlyFolders := collectSplitFolderGuardFolders(parentReq.Query, parentReq.WorkflowContextPaths)
		if len(fileContextWriteFolders) > 0 {
			log.Printf("[DELEGATION] Extracted write folder-guard paths from parent @context: %v", fileContextWriteFolders)
		}
		if len(workflowReadOnlyFolders) > 0 {
			log.Printf("[DELEGATION] Extracted read-only folder-guard paths from parent #workflow: %v", workflowReadOnlyFolders)
		}

		// Apply per-user folder guard for all sub-agents.
		// Writes scoped to _users/<subAgentUserID>/Chats/ and _users/<subAgentUserID>/memories/.
		{
			subPerUserChatsFolder := perUserChatsFolderFor(subAgentUserID)
			subPerUserChatsWrite := subPerUserChatsFolder + "/"
			subPerUserMemWrite := perUserMemoryFolderFor(subAgentUserID) + "/"
			subPerUserChatHistory := strings.TrimSuffix(subPerUserChatsFolder, "Chats") + "chat_history/"
			extraFolders := append([]string{"config/"}, subResolvedGrants.WriteFolders...)
			extraFolders = append(extraFolders, fileContextWriteFolders...)
			extraFolders = append(extraFolders, subPerUserMemWrite)
			extraFolders = append(extraFolders, subPerUserChatHistory)
			// Delegation path has no #workflow-derived blocked-write prefix (the parent
			// session's blocked paths aren't inherited here; this call path is for sub-agents
			// spawned with their own folder scope). Pass nil.
			workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors, workflowReadOnlyFolders, nil, extraFolders...)
			workspace.SetSessionWorkingDir(sessionID, "")
			readPaths := append([]string{subPerUserChatsWrite, subPerUserChatHistory, "Downloads/", "skills/", "subagents/", "Workflow/", "config/", subPerUserMemWrite}, extraFolders...)
			readPaths = append(readPaths, subResolvedGrants.ReadOnlyExtra...)
			readPaths = append(readPaths, workflowReadOnlyFolders...)
			workspace.SetSessionFolderGuard(sessionID,
				readPaths,
				append([]string{subPerUserChatsWrite, "Downloads/", "config/", subPerUserMemWrite, subPerUserChatHistory}, extraFolders...),
			)
		}

		// Register workspace tools
		for _, tool := range workspaceTools {
			if tool.Function == nil {
				continue
			}
			toolName := tool.Function.Name
			if executor, exists := workspaceExecutors[toolName]; exists {
				enhancedDescription := enhanceToolDescriptionForChatMode(toolName, tool.Function.Description, perUserChatsFolderFor(subAgentUserID))

				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					continue
				}

				toolCategory := toolCategories[toolName]
				if toolCategory == "" {
					continue
				}

				if virtualtools.IsImageTool(toolName) && parentReq.ImageGenConfig != nil {
					executor = virtualtools.WrapImageToolExecutorWithRuntimeOverride(executor, virtualtools.ImageGenRuntimeOverride{
						Provider: parentReq.ImageGenConfig.Provider,
						ModelID:  parentReq.ImageGenConfig.ModelID,
						APIKey:   parentReq.ImageGenConfig.APIKey,
					})
				}

				if err := underlyingAgent.RegisterCustomTool(
					toolName,
					enhancedDescription,
					params,
					executor,
					toolCategory,
				); err != nil {
					log.Printf("[DELEGATION] Warning: Failed to register tool %s for sub-agent: %v", toolName, err)
				}
			}
		}

		// Register browser tools if enabled
		if parentReq.EnableBrowserAccess != nil && *parentReq.EnableBrowserAccess {
			browserTools := virtualtools.CreateWorkspaceBrowserTools()
			browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, getCdpPort(parentReq))
			browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()

			browserExtraFolders := append([]string{}, subResolvedGrants.WriteFolders...)
			browserExtraFolders = append(browserExtraFolders, fileContextWriteFolders...)
			browserExecutors = wrapExecutorsWithChatModeFolderGuard(browserExecutors, workflowReadOnlyFolders, nil, browserExtraFolders...)

			for _, tool := range browserTools {
				if tool.Function == nil {
					continue
				}
				toolName := tool.Function.Name
				if executor, exists := browserExecutors[toolName]; exists {
					var params map[string]interface{}
					if tool.Function.Parameters != nil {
						paramsBytes, err := json.Marshal(tool.Function.Parameters)
						if err == nil {
							json.Unmarshal(paramsBytes, &params)
						}
					}
					if params == nil {
						continue
					}

					if err := underlyingAgent.RegisterCustomTool(
						toolName,
						tool.Function.Description,
						params,
						executor,
						browserCategory,
					); err != nil {
						log.Printf("[DELEGATION] Warning: Failed to register browser tool %s for sub-agent: %v", toolName, err)
					}
				}
			}
		}

		// NOTE: Sub-agents do NOT get the delegate tool themselves (v1 design choice)
		// This prevents runaway delegation chains.

		// Minimal worker context — tells the sub-agent its role and output conventions.
		subWorkerChatsFolder := perUserChatsFolderFor(subAgentUserID)
		underlyingAgent.AppendSystemPrompt(fmt.Sprintf(`## Your Role
You are a focused background worker. Complete the assigned task using available tools and return a clear, concise result.
- You cannot spawn further sub-agents
- You have no shared memory with the caller — all context is in the instruction you received
- Save any output files under %s/ (use the sub-folder specified in your instruction, or create a descriptive one if none is given)
- Return a summary of what you did and what you found`, subWorkerChatsFolder))
		log.Printf("[DELEGATION] Added worker context to sub-agent (chats=%s)", subWorkerChatsFolder)
	}

	log.Printf("[DELEGATION] Sub-agent created, executing instruction at depth %d", currentDepth)

	// Notify caller that the sub-agent wrapper is ready (used by background agents)
	if len(onCreated) > 0 && onCreated[0] != nil {
		onCreated[0](subAgent)
	}

	// Clean up isolated browser session when sub-agent finishes
	if subAgentSessionID != sessionID {
		defer func() {
			mcpagent.CloseSession(subAgentSessionID)
			log.Printf("[DELEGATION] Closed isolated browser session %s", subAgentSessionID)
		}()
	}

	// Run the sub-agent with the instruction
	startTime := time.Now()
	result, err := subAgent.Invoke(ctx, instruction)
	duration := time.Since(startTime)

	// Collect metrics from sub-agent
	metrics := subAgent.GetMetricsSnapshot()
	stats := &delegationEndStats{
		InputTokens:  metrics.InputTokens,
		OutputTokens: metrics.OutputTokens,
		ToolCalls:    metrics.ToolCallsExecuted,
		Duration:     duration.String(),
		TotalCostUSD: metrics.TotalCostUSD,
	}

	if err != nil {
		api.emitDelegationEndEvent(sessionID, delegationID, currentDepth, "", err.Error(), stats)
		return "", fmt.Errorf("sub-agent execution failed: %w", err)
	}

	// Emit delegation_end event with success
	api.emitDelegationEndEvent(sessionID, delegationID, currentDepth, fmt.Sprintf("Completed in %s", duration), "", stats)

	log.Printf("[DELEGATION] Sub-agent completed at depth %d in %s", currentDepth, duration)
	return result, nil
}

// --- Background Agent Infrastructure for Async Delegation ---

// bgAgentQuerierImpl implements virtualtools.BGAgentQuerier using the registry
type bgAgentQuerierImpl struct {
	registry *BackgroundAgentRegistry
}

func (q *bgAgentQuerierImpl) QueryAgent(sessionID, agentID string, last, offset int) (*virtualtools.BGAgentInfo, error) {
	agent := q.registry.Get(sessionID, agentID)
	if agent == nil {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}
	snap := agent.GetSnapshot()
	elapsed := time.Since(snap.CreatedAt)
	if snap.CompletedAt != nil {
		elapsed = snap.CompletedAt.Sub(snap.CreatedAt)
	}
	info := &virtualtools.BGAgentInfo{
		ID:        snap.ID,
		Name:      snap.Name,
		Status:    string(snap.Status),
		Elapsed:   elapsed.Truncate(time.Second).String(),
		CreatedAt: snap.CreatedAt.Format(time.RFC3339),
	}
	if snap.CompletedAt != nil {
		info.CompletedAt = snap.CompletedAt.Format(time.RFC3339)
	}
	if snap.Status == BGAgentCompleted || snap.Status == BGAgentFailed {
		info.Result = truncateForToolResponse(snap.Result, 4000)
		info.Error = snap.Error
	}
	if snap.Status == BGAgentRunning {
		// Return conversation history with pagination (last N entries, skip offset from end)
		agent := q.registry.Get(sessionID, agentID)
		if agent != nil {
			// Get more entries than needed so we can apply offset
			allHistory := agent.GetRecentHistory(last + offset)
			// Apply offset: trim the last `offset` entries
			if offset > 0 && len(allHistory) > offset {
				allHistory = allHistory[:len(allHistory)-offset]
			} else if offset > 0 {
				allHistory = nil // offset exceeds history length
			}
			// Take only the last `last` entries
			if len(allHistory) > last {
				allHistory = allHistory[len(allHistory)-last:]
			}
			for _, h := range allHistory {
				info.RecentHistory = append(info.RecentHistory, virtualtools.BGAgentHistoryEntry{
					Role: h.Role,
					Text: truncateForToolResponse(h.Text, 1000),
				})
			}
		}
		// Include recent tool calls with timing
		if agent != nil {
			toolCalls := agent.GetRecentToolCalls(5)
			for _, tc := range toolCalls {
				dur := ""
				if tc.Status == "running" {
					dur = time.Since(tc.StartedAt).Truncate(time.Second).String()
				} else if tc.Duration > 0 {
					dur = tc.Duration.Truncate(time.Millisecond).String()
				}
				info.RecentToolCalls = append(info.RecentToolCalls, virtualtools.BGAgentToolCall{
					ToolName: tc.ToolName,
					Duration: dur,
					Status:   tc.Status,
				})
			}
		}
	}
	return info, nil
}

func (q *bgAgentQuerierImpl) ListAgents(sessionID string) ([]*virtualtools.BGAgentInfo, error) {
	agents := q.registry.GetAll(sessionID)
	infos := make([]*virtualtools.BGAgentInfo, 0, len(agents))
	for _, agent := range agents {
		snap := agent.GetSnapshot()
		elapsed := time.Since(snap.CreatedAt)
		if snap.CompletedAt != nil {
			elapsed = snap.CompletedAt.Sub(snap.CreatedAt)
		}
		info := &virtualtools.BGAgentInfo{
			ID:      snap.ID,
			Name:    snap.Name,
			Status:  string(snap.Status),
			Elapsed: elapsed.Truncate(time.Second).String(),
		}
		if snap.Status == BGAgentFailed {
			info.Error = snap.Error
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (q *bgAgentQuerierImpl) TerminateAgent(sessionID, agentID string) error {
	return q.registry.CancelAgent(sessionID, agentID)
}

// workshopExecutionBgNotifier implements WorkshopExecutionNotifier by registering
// workshop step/background executions in bgAgentRegistry so that HasRunningAgents()
// returns true and the frontend keeps polling for events.
type workshopExecutionBgNotifier struct {
	api           *StreamingAPI
	sessionID     string
	workspacePath string
	presetQueryID string
	userID        string
}

func (n *workshopExecutionBgNotifier) OnExecutionStart(start todo_creation_human.WorkshopExecutionStart) {
	if n.api.isSessionMarkedStopped(n.sessionID) || n.api.isSessionStoppedOrInactive(n.sessionID) {
		log.Printf("[BG AGENT] OnExecutionStart ignored for stopped session %s (exec=%s)", n.sessionID, start.ID)
		if start.Cancel != nil {
			start.Cancel()
		}
		return
	}
	kind := strings.TrimSpace(start.Kind)
	if kind == "" {
		kind = "workshop_background"
	}
	bgAgent := &BackgroundAgent{
		ID:                start.ID,
		ParentExecutionID: start.ParentExecutionID,
		Name:              start.Name,
		SessionID:         n.sessionID,
		Kind:              kind,
		Status:            BGAgentRunning,
		CreatedAt:         time.Now(),
		cancel:            start.Cancel,
		Metadata: map[string]string{
			"workflow_path":    n.workspacePath,
			"preset_query_id":  n.presetQueryID,
			"execution_source": trackedExecutionSourceWorkshopBackground,
		},
	}
	n.api.bgAgentRegistry.Register(n.sessionID, bgAgent)
	n.api.trackWorkshopExecutionStart(n.sessionID, n.workspacePath, n.presetQueryID, n.userID, start.ID, start.Name)

	// Pre-create the channel so NotifyCompletion never drops a completion
	n.api.bgAgentRegistry.GetNotificationChannel(n.sessionID)

	// Ensure background completion loop is running
	n.api.completionLoopStartedMu.Lock()
	if !n.api.completionLoopStarted[n.sessionID] {
		n.api.completionLoopStarted[n.sessionID] = true
		go n.api.backgroundCompletionLoop(n.sessionID)
	}
	n.api.completionLoopStartedMu.Unlock()

	// Emit background_agent_started event so BackgroundAgentsStatusBar shows a pill
	n.api.emitBackgroundAgentEvent(n.sessionID, start.ID, "background_agent_started", map[string]interface{}{
		"agent_id": start.ID,
		"name":     start.Name,
	})
}

func (n *workshopExecutionBgNotifier) OnExecutionComplete(execID, name, result string, meta map[string]string, err error) {
	if n.api.isSessionMarkedStopped(n.sessionID) || n.api.isSessionStoppedOrInactive(n.sessionID) {
		n.api.completeTrackedExecution(execID, trackedExecutionStatusCanceled, "session stopped", meta)
		log.Printf("[BG AGENT] OnExecutionComplete ignored for stopped session %s (exec=%s)", n.sessionID, execID)
		return
	}
	agent := n.api.bgAgentRegistry.Get(n.sessionID, execID)
	if agent == nil {
		return
	}

	// If agent was already canceled (by CancelAll during workflow stop), treat as
	// terminated — don't emit completion events or trigger notification.
	if agent.GetStatus() == BGAgentCanceled {
		log.Printf("[BG AGENT] OnExecutionComplete skipped for already-canceled agent %s", execID)
		return
	}

	// Context-canceled errors indicate the parent workflow was stopped.
	// Treat these as cancellations even if CancelAll hasn't marked this specific
	// agent yet (e.g. it was registered after CancelAll ran).
	if err != nil && (strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "context deadline exceeded")) {
		agent.SetCanceled()
		n.api.completeTrackedExecution(execID, trackedExecutionStatusCanceled, err.Error(), meta)
		log.Printf("[BG AGENT] OnExecutionComplete treating context-canceled agent %s as terminated", execID)
		n.api.emitBackgroundAgentEvent(n.sessionID, execID, "background_agent_terminated", map[string]interface{}{
			"agent_id": execID,
			"name":     name,
		})
		return
	}

	duration := time.Since(agent.CreatedAt)
	if len(meta) > 0 {
		agent.SetMetadata(meta)
	}
	if err != nil {
		agent.SetError(err.Error())
		n.api.completeTrackedExecution(execID, trackedExecutionStatusFailed, err.Error(), meta)
		n.api.emitBackgroundAgentEvent(n.sessionID, execID, "background_agent_completed", map[string]interface{}{
			"agent_id": execID,
			"name":     name,
			"status":   "failed",
			"error":    err.Error(),
			"duration": duration.Truncate(time.Second).String(),
		})
	} else {
		agent.SetResult(result) // Store full result — truncation only happens at display/notification time
		n.api.completeTrackedExecution(execID, trackedExecutionStatusCompleted, "", meta)
		n.api.emitBackgroundAgentEvent(n.sessionID, execID, "background_agent_completed", map[string]interface{}{
			"agent_id": execID,
			"name":     name,
			"status":   "completed",
			"result":   truncateForToolResponse(result, 500),
			"duration": duration.Truncate(time.Second).String(),
		})
	}

	// Signal completion to the notification loop (triggers auto-notification synthetic turn)
	n.api.bgAgentRegistry.NotifyCompletion(n.sessionID, execID)
}

func (n *workshopExecutionBgNotifier) OnExecutionTerminated(execID, name string) {
	if n.api.isSessionMarkedStopped(n.sessionID) || n.api.isSessionStoppedOrInactive(n.sessionID) {
		n.api.completeTrackedExecution(execID, trackedExecutionStatusCanceled, "session stopped", nil)
		return
	}
	agent := n.api.bgAgentRegistry.Get(n.sessionID, execID)
	if agent == nil {
		return
	}
	agent.SetCanceled()
	n.api.completeTrackedExecution(execID, trackedExecutionStatusCanceled, "execution terminated", nil)
	n.api.emitBackgroundAgentEvent(n.sessionID, execID, "background_agent_terminated", map[string]interface{}{
		"agent_id": execID,
		"name":     name,
	})
	// Signal completion so the loop can process any pending completions
	n.api.bgAgentRegistry.NotifyCompletion(n.sessionID, execID)
}

// workflowSubAgentTrackingNotifier tracks inner workshop sub-agents in the backend
// execution tree without triggering synthetic-turn notifications.
type workflowSubAgentTrackingNotifier struct {
	api       *StreamingAPI
	sessionID string
}

func (n *workflowSubAgentTrackingNotifier) OnSubAgentStart(start todo_creation_human.WorkshopExecutionStart) {
	if n == nil || n.api == nil || strings.TrimSpace(start.ID) == "" {
		return
	}
	if n.api.isSessionMarkedStopped(n.sessionID) || n.api.isSessionStoppedOrInactive(n.sessionID) {
		if start.Cancel != nil {
			start.Cancel()
		}
		return
	}
	kind := strings.TrimSpace(start.Kind)
	if kind == "" {
		kind = "workflow_sub_agent"
	}
	bgAgent := &BackgroundAgent{
		ID:                start.ID,
		ParentExecutionID: start.ParentExecutionID,
		Name:              start.Name,
		SessionID:         n.sessionID,
		Kind:              kind,
		Status:            BGAgentRunning,
		CreatedAt:         time.Now(),
		cancel:            start.Cancel,
	}
	n.api.bgAgentRegistry.Register(n.sessionID, bgAgent)
}

func (n *workflowSubAgentTrackingNotifier) OnSubAgentComplete(agentID, _ string, result string, err error) {
	if n == nil || n.api == nil || strings.TrimSpace(agentID) == "" {
		return
	}
	if n.api.isSessionMarkedStopped(n.sessionID) || n.api.isSessionStoppedOrInactive(n.sessionID) {
		return
	}
	agent := n.api.bgAgentRegistry.Get(n.sessionID, agentID)
	if agent == nil {
		return
	}
	if agent.GetStatus() == BGAgentCanceled {
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "context deadline exceeded") {
			agent.SetCanceled()
			return
		}
		agent.SetError(err.Error())
		return
	}
	agent.SetResult(result)
}

// truncateForToolResponse truncates a string for inclusion in tool responses
func truncateForToolResponse(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

// executeBackgroundDelegatedTask spawns a background goroutine for async delegation
func (api *StreamingAPI) executeBackgroundDelegatedTask(
	ctx context.Context, parentReq QueryRequest, sessionID, name, instruction string,
) (string, error) {
	agentID := api.bgAgentRegistry.NextID(name)
	bgCtx, bgCancel := context.WithCancel(context.Background())
	parentExecutionID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)

	// Copy only the context values actually needed by executeDelegatedTask.
	// Note: DelegationDepthKey is NOT copied because background sub-agents don't have
	// the delegate tool, so they can never create further sub-agents.
	if rl, ok := ctx.Value(virtualtools.ReasoningLevelKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.ReasoningLevelKey, rl)
	}
	if at, ok := ctx.Value(virtualtools.AgentTemplateKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.AgentTemplateKey, at)
	}
	// Propagate per-user memory + chats folders to background sub-agents so their
	// shell commands against memories/ and Chats/ resolve to the right user's folder.
	if mf, ok := ctx.Value(virtualtools.MemoryFolderKey).(string); ok && mf != "" {
		bgCtx = context.WithValue(bgCtx, virtualtools.MemoryFolderKey, mf)
	}
	if cf, ok := ctx.Value(virtualtools.ChatsFolderKey).(string); ok && cf != "" {
		bgCtx = context.WithValue(bgCtx, virtualtools.ChatsFolderKey, cf)
	}
	if ds, ok := ctx.Value(virtualtools.DelegationServersKey).([]string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.DelegationServersKey, ds)
	}
	if sb, ok := ctx.Value(virtualtools.ShareBrowserKey).(bool); ok && !sb {
		bgCtx = context.WithValue(bgCtx, virtualtools.ShareBrowserKey, false)
	}
	// Pass user ID for per-user OAuth
	if userID, ok := ctx.Value(common.UserIDKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, common.UserIDKey, userID)
		log.Printf("[USER_ID_DEBUGGING] Background agent: copied UserIDKey=%q to bgCtx", userID)
	}

	bgAgent := &BackgroundAgent{
		ID:                agentID,
		ParentExecutionID: parentExecutionID,
		Name:              name,
		SessionID:         sessionID,
		Instruction:       instruction,
		Kind:              "delegation",
		Status:            BGAgentRunning,
		CreatedAt:         time.Now(),
		cancel:            bgCancel,
	}
	api.bgAgentRegistry.Register(sessionID, bgAgent)

	// Inject background agent ID so delegation_start event can link back to this agent
	bgCtx = context.WithValue(bgCtx, virtualtools.BackgroundAgentIDKey, agentID)

	// Inject tool event callback so executeDelegatedTask's observer tracks timing on bgAgent
	bgCtx = context.WithValue(bgCtx, virtualtools.ToolEventCallbackKey, events.ToolEventCallback(
		func(toolCallID, toolName, eventType string, duration time.Duration) {
			switch eventType {
			case "start":
				bgAgent.RecordToolCallStart(toolCallID, toolName)
			case "end":
				bgAgent.RecordToolCallEnd(toolCallID, toolName, duration, false)
			case "error":
				bgAgent.RecordToolCallEnd(toolCallID, toolName, duration, true)
			}
		},
	))

	// Emit background_agent_started event
	api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_started", map[string]interface{}{
		"agent_id":    agentID,
		"name":        name,
		"instruction": truncateForToolResponse(instruction, 200),
	})

	// Start the background completion loop for this session if not already running
	api.completionLoopStartedMu.Lock()
	if !api.completionLoopStarted[sessionID] {
		api.completionLoopStarted[sessionID] = true
		go api.backgroundCompletionLoop(sessionID)
	}
	api.completionLoopStartedMu.Unlock()

	go func() {
		defer bgCancel()
		result, err := api.executeDelegatedTask(bgCtx, parentReq, sessionID, instruction, func(wrapper *agent.LLMAgentWrapper) {
			// Attach history func so query_agent can read the sub-agent's live conversation
			bgAgent.SetHistoryFunc(func(lastN int) []HistoryEntry {
				history := wrapper.GetHistory()
				start := 0
				if lastN > 0 && len(history) > lastN {
					start = len(history) - lastN
				}
				var entries []HistoryEntry
				for _, msg := range history[start:] {
					role := string(msg.Role)
					var parts []string
					for _, part := range msg.Parts {
						switch p := part.(type) {
						case llmtypes.TextContent:
							if p.Text != "" {
								parts = append(parts, p.Text)
							}
						case llmtypes.ToolCall:
							name := ""
							args := ""
							if p.FunctionCall != nil {
								name = p.FunctionCall.Name
								args = p.FunctionCall.Arguments
							}
							parts = append(parts, fmt.Sprintf("[tool_call: %s(%s)]", name, args))
						case *llmtypes.ToolCall:
							name := ""
							args := ""
							if p != nil && p.FunctionCall != nil {
								name = p.FunctionCall.Name
								args = p.FunctionCall.Arguments
							}
							parts = append(parts, fmt.Sprintf("[tool_call: %s(%s)]", name, args))
						case llmtypes.ToolCallResponse:
							parts = append(parts, fmt.Sprintf("[tool_result: %s] %s", p.Name, p.Content))
						case *llmtypes.ToolCallResponse:
							if p != nil {
								parts = append(parts, fmt.Sprintf("[tool_result: %s] %s", p.Name, p.Content))
							}
						}
					}
					if len(parts) > 0 {
						entries = append(entries, HistoryEntry{
							Role: role,
							Text: strings.Join(parts, "\n"),
						})
					}
				}
				return entries
			})
		})

		now := time.Now()
		duration := now.Sub(bgAgent.CreatedAt)

		if err != nil {
			bgAgent.SetError(err.Error())
			api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_completed", map[string]interface{}{
				"agent_id": agentID,
				"name":     name,
				"status":   "failed",
				"error":    err.Error(),
				"duration": duration.Truncate(time.Second).String(),
			})
			log.Printf("[BG AGENT] Agent '%s' (ID: %s) failed after %s: %v", name, agentID, duration, err)
		} else {
			bgAgent.SetResult(result)
			api.emitBackgroundAgentEvent(sessionID, agentID, "background_agent_completed", map[string]interface{}{
				"agent_id": agentID,
				"name":     name,
				"status":   "completed",
				"result":   truncateForToolResponse(result, 500),
				"duration": duration.Truncate(time.Second).String(),
			})
			log.Printf("[BG AGENT] Agent '%s' (ID: %s) completed in %s", name, agentID, duration)
		}

		// Signal completion to the notification loop
		api.bgAgentRegistry.NotifyCompletion(sessionID, agentID)
	}()

	return agentID, nil
}

// emitBackgroundAgentEvent emits a background agent event to the event store
func (api *StreamingAPI) emitBackgroundAgentEvent(sessionID, agentID, eventType string, data map[string]interface{}) {
	now := time.Now()
	data["timestamp"] = now.Format(time.RFC3339)
	if _, exists := data["parent_execution_id"]; !exists && api != nil && api.bgAgentRegistry != nil && agentID != "" {
		if agent := api.bgAgentRegistry.Get(sessionID, agentID); agent != nil {
			if parentID := strings.TrimSpace(agent.GetSnapshot().ParentExecutionID); parentID != "" {
				data["parent_execution_id"] = parentID
			}
		}
	}

	eventID := fmt.Sprintf("%s_%s_%s", sessionID, eventType, agentID)
	if agentID == "" {
		eventID = fmt.Sprintf("%s_%s_%d", sessionID, eventType, now.UnixNano())
	}

	event := events.Event{
		ID:        eventID,
		Type:      eventType,
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      unifiedevents.EventType(eventType),
			Timestamp: now,
			SessionID: sessionID,
			Component: "background-agent",
			Data:      events.NewGenericEventData(eventType, data),
		},
	}
	api.eventStore.AddEvent(sessionID, event)
}

// isSessionBusy returns whether the session is currently processing a user turn
func (api *StreamingAPI) isSessionBusy(sessionID string) bool {
	api.sessionBusyMu.RLock()
	defer api.sessionBusyMu.RUnlock()
	return api.sessionBusy[sessionID]
}

// setSessionBusy sets the busy state for a session
func (api *StreamingAPI) setSessionBusy(sessionID string, busy bool) {
	api.sessionBusyMu.Lock()
	api.sessionBusy[sessionID] = busy
	api.sessionBusyMu.Unlock()
}

// isSessionStoppedOrInactive returns true when a session has been explicitly stopped
// or aged out, in which case background completions must not trigger synthetic turns.
func (api *StreamingAPI) isSessionStoppedOrInactive(sessionID string) bool {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	session, exists := api.activeSessions[sessionID]
	if !exists {
		return false
	}
	return session.Status == "stopped" || session.Status == "inactive"
}

// markSessionStopped records that a user explicitly stopped this session.
// In-flight goroutines check this before spawning new workshop sessions or
// step execution processes. See stoppedSessions field comment for full bug description.
func (api *StreamingAPI) markSessionStopped(sessionID string) {
	api.stoppedSessionsMu.Lock()
	api.stoppedSessions[sessionID] = true
	api.stoppedSessionsMu.Unlock()
}

// clearSessionStopped removes the stopped guard so the session can accept new queries.
// Called when a NEW user message explicitly reactivates the session (not by racing goroutines).
func (api *StreamingAPI) clearSessionStopped(sessionID string) {
	api.stoppedSessionsMu.Lock()
	delete(api.stoppedSessions, sessionID)
	api.stoppedSessionsMu.Unlock()
}

// isSessionMarkedStopped returns true if the user explicitly stopped this session
// and no new user message has reactivated it yet.
func (api *StreamingAPI) isSessionMarkedStopped(sessionID string) bool {
	api.stoppedSessionsMu.RLock()
	defer api.stoppedSessionsMu.RUnlock()
	return api.stoppedSessions[sessionID]
}

// setSyntheticTurn marks a session as running an auto-notification synthetic turn.
// The frontend uses this to avoid blocking user input during background agent notifications.
func (api *StreamingAPI) setSyntheticTurn(sessionID string, synthetic bool) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()
	if session, exists := api.activeSessions[sessionID]; exists {
		session.IsSyntheticTurn = synthetic
	}
}

// isSyntheticTurn returns true if the session is currently running a synthetic (auto-notification) turn.
func (api *StreamingAPI) isSyntheticTurn(sessionID string) bool {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	if session, exists := api.activeSessions[sessionID]; exists {
		return session.IsSyntheticTurn
	}
	return false
}

// queuePendingCompletion adds a completed agent ID to the pending queue
func (api *StreamingAPI) queuePendingCompletion(sessionID, agentID string) {
	api.pendingMu.Lock()
	defer api.pendingMu.Unlock()
	api.pendingCompletions[sessionID] = append(api.pendingCompletions[sessionID], agentID)
}

// drainPendingCompletions returns and clears all pending completion agent IDs
func (api *StreamingAPI) drainPendingCompletions(sessionID string) []string {
	api.pendingMu.Lock()
	defer api.pendingMu.Unlock()
	pending := api.pendingCompletions[sessionID]
	delete(api.pendingCompletions, sessionID)
	return pending
}

// backgroundCompletionLoop listens for background agent completions and triggers synthetic turns
func (api *StreamingAPI) backgroundCompletionLoop(sessionID string) {
	ch := api.bgAgentRegistry.GetNotificationChannel(sessionID)
	log.Printf("[BG AGENT] Started completion loop for session %s", sessionID)
	defer func() {
		api.completionLoopStartedMu.Lock()
		delete(api.completionLoopStarted, sessionID)
		api.completionLoopStartedMu.Unlock()
		log.Printf("[BG AGENT] Completion loop ended for session %s", sessionID)
	}()

	for agentID := range ch {
		if api.isSessionStoppedOrInactive(sessionID) {
			log.Printf("[BG AGENT] Session %s is stopped/inactive, dropping completion for agent %s", sessionID, agentID)
			continue
		}
		if api.isSessionBusy(sessionID) {
			// Session is busy — queue the completion for later processing
			api.queuePendingCompletion(sessionID, agentID)
			log.Printf("[BG AGENT] Session %s busy, queued completion for agent %s", sessionID, agentID)
		} else {
			api.processBackgroundAgentCompletion(sessionID, agentID)
		}
	}
}

// processBatchedBackgroundAgentCompletions builds a single [AUTO-NOTIFICATION] message for one or more
// completed agents and fires ONE synthetic turn. Subsequent drained completions are chained via
// the synthetic turn's own defer, avoiding concurrent StreamWithEvents calls.
func (api *StreamingAPI) processBatchedBackgroundAgentCompletions(sessionID string, agentIDs []string) {
	if len(agentIDs) == 0 {
		return
	}
	if api.isSessionStoppedOrInactive(sessionID) {
		log.Printf("[BG AGENT] Session %s is stopped/inactive, skipping %d batched completion(s)", sessionID, len(agentIDs))
		return
	}

	// Single completion: use the normal individual path (simpler message).
	if len(agentIDs) == 1 {
		api.processBackgroundAgentCompletion(sessionID, agentIDs[0])
		return
	}

	// Multiple completions: build a batched [AUTO-NOTIFICATION] message.
	var parts []string
	var emittedIDs []string
	for _, agentID := range agentIDs {
		agent := api.bgAgentRegistry.Get(sessionID, agentID)
		if agent == nil {
			continue
		}
		agent.mu.Lock()
		if agent.notified {
			agent.mu.Unlock()
			continue
		}
		agent.notified = true
		agent.mu.Unlock()

		snap := agent.GetSnapshot()
		if snap.Status == BGAgentCanceled {
			continue
		}
		var resultText string
		if snap.Status == BGAgentCompleted {
			resultText = snap.Result // Full result — no truncation in auto-notification
		} else if snap.Status == BGAgentFailed {
			resultText = fmt.Sprintf("Error: %s", snap.Error)
		} else {
			resultText = fmt.Sprintf("Status: %s", snap.Status)
		}
		workshopMode := ""
		isLockCode := false
		isLockLearnings := false
		lockCodeConsecutiveFailures := 0
		lockCodeNeedsReview := false
		if snap.Metadata != nil {
			workshopMode = snap.Metadata["workshop_mode"]
			isLockCode = snap.Metadata["lock_code"] == "true"
			isLockLearnings = snap.Metadata["lock_learnings"] == "true"
			if v := snap.Metadata["lock_code_consecutive_failures"]; v != "" {
				if n, perr := strconv.Atoi(v); perr == nil {
					lockCodeConsecutiveFailures = n
				}
			}
			lockCodeNeedsReview = snap.Metadata["lock_code_needs_review"] == "true"
		}
		actionHint := buildWorkshopActionHint(workshopMode, isLockCode, isLockLearnings, lockCodeConsecutiveFailures, lockCodeNeedsReview, snap.Status == BGAgentFailed)
		batchContext := ""
		if snap.Metadata != nil {
			if iter, ok := snap.Metadata["iteration"]; ok && iter != "" {
				batchContext += fmt.Sprintf(" [%s", iter)
				if gid, ok := snap.Metadata["group_name"]; ok && gid != "" {
					batchContext += "/" + gid
				}
				batchContext += "]"
			}
		}
		parts = append(parts, fmt.Sprintf("- **%s** (ID: %s)%s: %s\n  Result: %s%s", snap.Name, snap.ID, batchContext, snap.Status, resultText, actionHint))
		emittedIDs = append(emittedIDs, agentID)
	}

	if len(parts) == 0 {
		return
	}

	syntheticMsg := fmt.Sprintf("[AUTO-NOTIFICATION] Multiple step completions:\n%s", strings.Join(parts, "\n"))
	if strings.HasPrefix(sessionID, "bot-") {
		syntheticMsg += botAutoNotificationProgressDirective(api.isFinalBotAutoNotification(sessionID))
	}

	// Emit synthetic_turn_ready event for each agent
	for _, agentID := range emittedIDs {
		api.emitBackgroundAgentEvent(sessionID, agentID, "synthetic_turn_ready", map[string]interface{}{
			"message":  "Background agents completed. The main agent will process the results.",
			"agent_id": agentID,
			"status":   "completed",
		})
	}

	api.executeSyntheticTurn(sessionID, syntheticMsg)
}

// buildWorkshopActionHint returns a mode-specific instruction appended to AUTO-NOTIFICATION messages
// so the agent knows what to do next. Most success/failure cases are handled by the system prompt;
// this function only adds extra guidance for cases where the engine has silently degraded behavior
// the orchestrator wouldn't otherwise know about — most notably fast-path failures on locked steps,
// where the fix loop is disabled and the step gets exactly one shot at running the saved main.py.
func buildWorkshopActionHint(workshopMode string, isLockCode, isLockLearnings bool, lockCodeConsecutiveFailures int, lockCodeNeedsReview, failed bool) string {
	if !failed {
		return ""
	}
	// Pattern hint shared by both locked-step branches: a streak of locked failures is
	// strong evidence the lock itself is wrong (script no longer matches the site/API),
	// not that each individual run is independently environmental.
	streakHint := ""
	if lockCodeNeedsReview || lockCodeConsecutiveFailures >= 3 {
		streakHint = fmt.Sprintf(
			"\n\n**Pattern signal:** the locked main.py has now failed %d times in a row "+
				"(`script_metadata.json.lock_code_stats.consecutive_failures=%d`, `needs_review=%v`). "+
				"At this point the lock is likely wrong — a single environmental failure is plausible, "+
				"three in a row usually means the saved script no longer matches the site/API. "+
				"Strongly consider clearing `lock_code` and patching the script rather than treating "+
				"this as one more transient failure.",
			lockCodeConsecutiveFailures, lockCodeConsecutiveFailures, lockCodeNeedsReview)
	}
	if isLockCode && isLockLearnings {
		return "\n\n[LOCKED STEP FAILED] This step is locked " +
			"(`lock_code=true`, `lock_learnings=true`) and ran on the fast path, " +
			"so only the saved main.py executed — no fix loop, no LLM repair attempt. " +
			"Investigate the failure: read the run folder " +
			"(`step_*_status.json`, `learn_code_fast_path.json`, screenshots, downloaded files) " +
			"and decide between two recovery paths:\n" +
			"  1. **Fix main.py** — if there's a real bug in the script (these accumulate over time as " +
			"sites and APIs change), clear `lock_code` via `update_step_config` and update the script. " +
			"Use `review_step_code` or rewrite directly based on what you find.\n" +
			"  2. **Re-run with `fast_path_only=false`** — calls `execute_step` again with the fast path " +
			"disabled so the full code_exec path engages. The LLM will drive tools directly, can repair " +
			"the run live, and (if `lock_code` is cleared) save an updated main.py back to learnings. " +
			"Good first move when you're not sure whether it's a script bug or environmental.\n" +
			"If after inspection it's clearly environmental (bad creds, MFA prompt, captcha) and the " +
			"script is fine, surface that to the user instead of touching the code." +
			streakHint
	}
	if isLockCode {
		return "\n\n[CODE-LOCKED FAILURE] `lock_code=true` so the fix loop is disabled and the saved " +
			"main.py is frozen. Inspect the run folder, then either clear `lock_code` and fix the " +
			"script, or re-run with `fast_path_only=false` to engage code_exec for this run." +
			streakHint
	}
	return ""
}

// processBackgroundAgentCompletion injects a synthetic message and triggers a new main agent turn
func (api *StreamingAPI) processBackgroundAgentCompletion(sessionID, agentID string) {
	if api.isSessionStoppedOrInactive(sessionID) {
		log.Printf("[BG AGENT] Session %s is stopped/inactive, skipping completion for agent %s", sessionID, agentID)
		return
	}
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	if agent == nil {
		log.Printf("[BG AGENT] Warning: agent %s not found for completion processing", agentID)
		return
	}

	// Prevent duplicate processing
	agent.mu.Lock()
	if agent.notified {
		agent.mu.Unlock()
		return
	}
	agent.notified = true
	agent.mu.Unlock()

	snap := agent.GetSnapshot()
	if snap.Status == BGAgentCanceled {
		log.Printf("[BG AGENT] Agent %s for session %s was canceled, suppressing synthetic turn", agentID, sessionID)
		return
	}

	var resultText string
	if snap.Status == BGAgentCompleted {
		resultText = snap.Result // Full result — no truncation in auto-notification
	} else if snap.Status == BGAgentFailed {
		resultText = fmt.Sprintf("Error: %s", snap.Error)
	} else {
		resultText = fmt.Sprintf("Status: %s", snap.Status)
	}

	// Append mode-specific action hint so the agent knows what to do next.
	workshopMode := ""
	isLockCode := false
	isLockLearnings := false
	lockCodeConsecutiveFailures := 0
	lockCodeNeedsReview := false
	if snap.Metadata != nil {
		workshopMode = snap.Metadata["workshop_mode"]
		isLockCode = snap.Metadata["lock_code"] == "true"
		isLockLearnings = snap.Metadata["lock_learnings"] == "true"
		if v := snap.Metadata["lock_code_consecutive_failures"]; v != "" {
			if n, perr := strconv.Atoi(v); perr == nil {
				lockCodeConsecutiveFailures = n
			}
		}
		lockCodeNeedsReview = snap.Metadata["lock_code_needs_review"] == "true"
	}
	isFailed := snap.Status == BGAgentFailed
	actionHint := buildWorkshopActionHint(workshopMode, isLockCode, isLockLearnings, lockCodeConsecutiveFailures, lockCodeNeedsReview, isFailed)

	// Include iteration and group_name if available in metadata
	contextInfo := ""
	if snap.Metadata != nil {
		if iter, ok := snap.Metadata["iteration"]; ok && iter != "" {
			contextInfo += fmt.Sprintf("\nIteration: %s", iter)
		}
		if gid, ok := snap.Metadata["group_name"]; ok && gid != "" {
			contextInfo += fmt.Sprintf("\nGroup: %s", gid)
		}
	}
	syntheticMsg := fmt.Sprintf(
		"[AUTO-NOTIFICATION]\nAgent '%s' (ID: %s) completed.\nStatus: %s%s\nResult:\n%s%s",
		snap.Name, snap.ID, snap.Status, contextInfo, resultText, actionHint)

	// Bot connector sessions (slack / whatsapp / discord / telegram / etc.): the
	// builder's reply is forwarded verbatim to a chat thread, so a faithful echo
	// of the full sub-agent result blows up the conversation. Append a brevity
	// directive so the builder still ingests the full result above (full context
	// for its own reasoning) but replies to the user with a single short status
	// line. Web / desktop sessions intentionally keep the verbose progressive
	// update — that long reply renders fine in a side panel, not in chat.
	// Session ID format is `bot-<platform>--<uuid>` (see newBotSessionID).
	if strings.HasPrefix(sessionID, "bot-") {
		syntheticMsg += botAutoNotificationProgressDirective(api.isFinalBotAutoNotification(sessionID))
	}

	// NOTE: Don't inject syntheticMsg into conversation history here.
	// handleQuery will add it via StreamWithEvents when the synthetic turn runs.

	// Emit synthetic_turn_ready event so frontend shows amber banner before the turn fires
	statusLabel := "completed"
	if snap.Status == BGAgentFailed {
		statusLabel = "failed"
	}
	api.emitBackgroundAgentEvent(sessionID, agentID, "synthetic_turn_ready", map[string]interface{}{
		"message":  fmt.Sprintf("Background agent '%s' %s. The main agent will process the results.", snap.Name, statusLabel),
		"agent_id": snap.ID,
		"name":     snap.Name,
		"status":   string(snap.Status),
	})

	// Trigger a synthetic turn using the stored QueryRequest
	// Called synchronously so handleQuery sets session busy before returning,
	// preventing concurrent synthetic turns for the same session.
	api.executeSyntheticTurn(sessionID, syntheticMsg)
}

func (api *StreamingAPI) isFinalBotAutoNotification(sessionID string) bool {
	if api.botManager == nil || !strings.HasPrefix(sessionID, "bot-") {
		return false
	}
	// Registrations are usually removed before the synthetic turn is injected.
	// Treat 0 or 1 remaining mirrored sessions as the terminal notification so
	// we stop adding the progress-only bot directive and let Run mode respond
	// from its normal prompt/context.
	return api.botManager.PendingWorkflowCount(sessionID) <= 1
}

func botAutoNotificationProgressDirective(final bool) string {
	if final {
		return ""
	}
	return "\n\n---\nYou are operating inside a chat thread on a bot connector. The user must be able to tell this is a workflow sub-agent progress update, not a normal workflow-builder chat reply. Reply with ONE short status line (target <=150 characters) using this exact prefix format: \"Step update (<sub-agent name>): <status headline>\". Use the Agent name or completion entry name from above as the sub-agent name. Do NOT start with only \"Status: completed\". Do NOT restate, quote, or summarize the full Result block above. Examples: \"Step update (search-score-jobs): completed - 15 jobs shortlisted from 56; enriching client data next.\" / \"Step update (route-query-logs): failed - rate limit hit; retrying with backoff.\""
}

// executeSyntheticTurn drives the stored agent directly with a synthetic message.
// Instead of creating an internal HTTP request and re-building the entire agent/tools/history,
// it reuses the agent stored after the last plan-mode turn via StreamWithEvents().
// This is called synchronously from processBackgroundAgentCompletion — it sets session busy
// before spawning the goroutine, preventing concurrent synthetic turns.
func (api *StreamingAPI) executeSyntheticTurn(sessionID, syntheticMsg string) {
	if api.isSessionStoppedOrInactive(sessionID) {
		log.Printf("[BG AGENT] Session %s is stopped/inactive, suppressing synthetic turn", sessionID)
		return
	}
	if api.botManager != nil {
		api.botManager.PrepareSyntheticTurn(sessionID)
	}
	// Get stored agent for this session
	api.sessionAgentsMux.RLock()
	llmAgent, ok := api.sessionAgents[sessionID]
	api.sessionAgentsMux.RUnlock()

	if !ok || llmAgent == nil {
		log.Printf("[BG AGENT] No stored agent for session %s, cannot trigger synthetic turn", sessionID)
		return
	}

	// Get stored query request for user ID context
	api.lastQueryMu.RLock()
	req, hasReq := api.lastQueryRequests[sessionID]
	api.lastQueryMu.RUnlock()

	// Set session busy synchronously BEFORE spawning goroutine
	// This prevents concurrent synthetic turns from the completion listener
	api.setSessionBusy(sessionID, true)

	// Mark as synthetic turn so frontend doesn't block user input
	api.setSyntheticTurn(sessionID, true)

	// Update session status to running
	api.updateSessionStatus(sessionID, "running")

	// Create cancellable context for this synthetic turn
	agentCtx, agentCancel := context.WithCancel(context.Background())

	// Inject user ID into context
	if hasReq && req.userID != "" {
		agentCtx = context.WithValue(agentCtx, common.UserIDKey, req.userID)
	}

	// Store cancel function so handleStopSession can cancel this turn
	api.agentCancelMux.Lock()
	api.agentCancelFuncs[sessionID] = agentCancel
	api.agentCancelMux.Unlock()

	log.Printf("[BG AGENT] Executing synthetic turn for session %s via stored agent", sessionID)

	go func() {
		defer func() {
			// Clean up cancel function
			api.agentCancelMux.Lock()
			delete(api.agentCancelFuncs, sessionID)
			api.agentCancelMux.Unlock()

			// Clear synthetic turn flag
			api.setSyntheticTurn(sessionID, false)

			// Clear session busy first so any later work sees the session as idle.
			api.setSessionBusy(sessionID, false)

			// If the session was explicitly stopped while this synthetic turn was running,
			// do not chain any queued completions. That would re-enter the stopped session.
			if api.isSessionStoppedOrInactive(sessionID) {
				log.Printf("[BG AGENT] Session %s stopped/inactive after synthetic turn, skipping pending completion drain", sessionID)
				return
			}

			// Drain queued completions only for still-active sessions (batched to avoid
			// concurrent StreamWithEvents calls).
			pending := api.drainPendingCompletions(sessionID)
			if len(pending) > 0 {
				go api.processBatchedBackgroundAgentCompletions(sessionID, pending)
			}
		}()

		// Stream the synthetic message through the stored agent
		// Events flow through already-attached EventObservers (in-memory + DB)
		textChan, err := llmAgent.StreamWithEvents(agentCtx, syntheticMsg)
		if err != nil {
			log.Printf("[BG AGENT] StreamWithEvents error for synthetic turn on session %s: %v", sessionID, err)
			api.updateSessionStatus(sessionID, "error")
			return
		}

		// Consume text chunks and save conversation history incrementally
		for range textChan {
			api.conversationMux.Lock()
			api.conversationHistory[sessionID] = llmAgent.GetHistory()
			api.conversationMux.Unlock()
		}

		// A stopped/canceled synthetic turn must not "complete" afterward, otherwise
		// it can resurrect the stored agent and reopen Playwright after Esc/stop.
		if agentCtx.Err() != nil || api.isSessionStoppedOrInactive(sessionID) {
			log.Printf("[BG AGENT] Synthetic turn aborted for session %s after stream end (ctx_err=%v stopped=%v)",
				sessionID, agentCtx.Err(), api.isSessionStoppedOrInactive(sessionID))
			return
		}

		// Final save of conversation history
		finalHistory := llmAgent.GetHistory()
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = finalHistory
		api.conversationMux.Unlock()
		log.Printf("[BG AGENT] Synthetic turn completed for session %s, history: %d messages", sessionID, len(finalHistory))

		// Persist conversation to builder/conversation/YYYY-MM-DD/ on disk (same as handleQuery defer)
		// Without this, auto-notification responses are only in memory and lost on restart.
		api.sessionWorkspaceMu.RLock()
		workflowPhaseFolder, hasFolderForSession := api.sessionWorkspaceFolders[sessionID]
		api.sessionWorkspaceMu.RUnlock()
		if hasFolderForSession && workflowPhaseFolder != "" && len(finalHistory) > 0 {
			phaseID := ""
			if hasReq {
				phaseID = strings.TrimSpace(req.PhaseID)
			}
			logPath := workflowBuilderConversationLogPath(workflowPhaseFolder, sessionID, time.Now())
			if phaseID == "" {
				if existingContent, exists, err := readFileFromWorkspace(context.Background(), logPath); err == nil && exists {
					var existing struct {
						PhaseID string `json:"phase_id"`
					}
					if json.Unmarshal([]byte(existingContent), &existing) == nil {
						phaseID = strings.TrimSpace(existing.PhaseID)
					}
				}
			}
			if phaseID == "" {
				phaseID = "workflow-builder"
			}
			convData := map[string]interface{}{
				"session_id":           sessionID,
				"phase_id":             phaseID,
				"conversation_history": finalHistory,
				"updated_at":           time.Now().Format(time.RFC3339),
			}
			if convJSON, err := json.MarshalIndent(convData, "", "  "); err == nil {
				if err := writeRawFileToWorkspace(context.Background(), logPath, string(convJSON)); err != nil {
					log.Printf("[BG AGENT] Failed to persist builder conversation after synthetic turn: %v", err)
				} else {
					log.Printf("[BG AGENT] Persisted builder conversation after synthetic turn (%d messages) to %s", len(finalHistory), logPath)
				}
			}
		}

		if api.botManager != nil && strings.HasPrefix(sessionID, "bot-") {
			finalText := latestAssistantTextFromHistory(finalHistory)
			api.botManager.SendSyntheticTurnFinalIfNeeded(sessionID, finalText)
		}

		// Update stored agent (it now has the latest history from this turn)
		api.sessionAgentsMux.Lock()
		api.sessionAgents[sessionID] = llmAgent
		api.sessionAgentsMux.Unlock()

		// Update session status to completed
		api.updateSessionStatus(sessionID, "completed")
	}()
}

// buildCapabilitiesContext creates a CapabilitiesContext from the chat request
// This is passed to the planner sub-agent so it knows what tools/servers/skills are available
func buildCapabilitiesContext(req QueryRequest) *virtualtools.CapabilitiesContext {
	hasBrowser := req.EnableBrowserAccess != nil && *req.EnableBrowserAccess
	caps := &virtualtools.CapabilitiesContext{
		EnabledServers: req.EnabledServers,
		SelectedTools:  req.SelectedTools,
		HasWorkspace:   true,
		HasBrowser:     hasBrowser,
	}

	// Load skill summaries
	workspaceAPIURL := getWorkspaceAPIURL()
	for _, folderName := range req.SelectedSkills {
		skill, err := skills.GetSkill(workspaceAPIURL, folderName)
		if err != nil {
			log.Printf("[CAPABILITIES] Warning: Failed to load skill %s: %v", folderName, err)
			continue
		}
		caps.Skills = append(caps.Skills, virtualtools.SkillSummary{
			Name:        skill.Frontmatter.Name,
			Description: skill.Frontmatter.Description,
			FolderName:  folderName,
		})
	}

	// Load sub-agent template summaries
	for _, folderName := range req.SelectedSubAgents {
		sa, err := subagents.GetSubAgent(workspaceAPIURL, folderName)
		if err != nil {
			log.Printf("[CAPABILITIES] Warning: Failed to load sub-agent template %s: %v", folderName, err)
			continue
		}
		caps.SubAgentTemplates = append(caps.SubAgentTemplates, virtualtools.SubAgentTemplateSummary{
			Name:                  sa.Frontmatter.Name,
			Description:           sa.Frontmatter.Description,
			FolderName:            folderName,
			DefaultReasoningLevel: sa.Frontmatter.DefaultReasoningLevel,
		})
	}

	return caps
}

// emitDelegationStartEvent emits an event when delegation starts
// This event serves as the parent for all sub-agent events (via parent_id linking)
func (api *StreamingAPI) emitDelegationStartEvent(sessionID, delegationID string, depth int, instruction, reasoningLevel, modelID string, servers []string, backgroundAgentID, agentTemplate string) {
	now := time.Now()
	eventID := fmt.Sprintf("%s_delegation_start_%s", sessionID, delegationID)
	eventData := &events.DelegationStartEventData{
		DelegationID:      delegationID,
		Depth:             depth,
		Instruction:       instruction,
		ReasoningLevel:    reasoningLevel,
		ModelID:           modelID,
		Servers:           servers,
		BackgroundAgentID: backgroundAgentID,
		AgentTemplate:     agentTemplate,
		Timestamp:         now.Format(time.RFC3339),
	}
	event := events.Event{
		ID:        eventID,
		Type:      "delegation_start",
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:           unifiedevents.EventType("delegation_start"),
			Timestamp:      now,
			HierarchyLevel: depth,
			SessionID:      sessionID,
			Component:      fmt.Sprintf("delegation-%d", depth),
			CorrelationID:  delegationID, // Links all delegation events together
			Data:           eventData,
		},
	}
	api.eventStore.AddEvent(sessionID, event)
	log.Printf("[DELEGATION] Emitted delegation_start event %s for %s at depth %d", eventID, delegationID, depth)
}

// delegationEndStats holds optional stats for delegation end events
type delegationEndStats struct {
	InputTokens  int64
	OutputTokens int64
	ToolCalls    int64
	Duration     string
	TotalCostUSD float64
}

// emitDelegationEndEvent emits an event when delegation ends
// This event has the same correlation_id as delegation_start for grouping
func (api *StreamingAPI) emitDelegationEndEvent(sessionID, delegationID string, depth int, result, errorMsg string, stats *delegationEndStats) {
	now := time.Now()
	delegationStartEventID := fmt.Sprintf("%s_delegation_start_%s", sessionID, delegationID)
	eventData := &events.DelegationEndEventData{
		DelegationID: delegationID,
		Depth:        depth,
		Result:       result,
		Error:        errorMsg,
		Success:      errorMsg == "",
		Timestamp:    now.Format(time.RFC3339),
	}
	if stats != nil {
		eventData.InputTokens = stats.InputTokens
		eventData.OutputTokens = stats.OutputTokens
		eventData.ToolCalls = stats.ToolCalls
		eventData.Duration = stats.Duration
		eventData.TotalCostUSD = stats.TotalCostUSD
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_delegation_end_%s", sessionID, delegationID),
		Type:      "delegation_end",
		Timestamp: now,
		SessionID: sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:           unifiedevents.EventType("delegation_end"),
			Timestamp:      now,
			HierarchyLevel: depth,
			SessionID:      sessionID,
			Component:      fmt.Sprintf("delegation-%d", depth),
			CorrelationID:  delegationID,           // Links all delegation events together
			ParentID:       delegationStartEventID, // Makes this a child of delegation_start (for tree display)
			Data:           eventData,
		},
	}
	api.eventStore.AddEvent(sessionID, event)
	log.Printf("[DELEGATION] Emitted delegation_end event for %s at depth %d (success: %v)", delegationID, depth, errorMsg == "")
}

func sanitizeTierModel(model *virtualtools.TierModel) *virtualtools.TierModel {
	if model == nil || model.Provider == "" || model.ModelID == "" {
		return nil
	}
	sanitized := &virtualtools.TierModel{
		Provider:  strings.TrimSpace(model.Provider),
		ModelID:   strings.TrimSpace(model.ModelID),
		Fallbacks: nil,
	}
	if len(model.Fallbacks) > 0 {
		for _, fb := range model.Fallbacks {
			modelID := strings.TrimSpace(fb.ModelID)
			if modelID == "" {
				continue
			}
			sanitized.Fallbacks = append(sanitized.Fallbacks, virtualtools.TierModelFallback{
				Provider: strings.TrimSpace(fb.Provider),
				ModelID:  modelID,
			})
		}
		if len(sanitized.Fallbacks) == 0 {
			sanitized.Fallbacks = nil
		}
	}
	return sanitized
}

func convertTierFallbacksToAgentFallbacks(fallbacks []virtualtools.TierModelFallback, defaultProvider string) []agent.FallbackModel {
	if len(fallbacks) == 0 {
		return nil
	}
	out := make([]agent.FallbackModel, 0, len(fallbacks))
	for _, fb := range fallbacks {
		modelID := strings.TrimSpace(fb.ModelID)
		if modelID == "" {
			continue
		}
		provider := strings.TrimSpace(fb.Provider)
		if provider == "" {
			provider = defaultProvider
		}
		out = append(out, agent.FallbackModel{
			Provider: provider,
			ModelID:  modelID,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveDelegationTierConfig builds a DelegationTierConfig by merging:
// 1. Frontend config (from QueryRequest) - highest priority
// 2. Environment variables (DELEGATION_TIER_*) - fallback
// Returns nil if no tier config is available at all
func resolveDelegationTierConfig(frontendConfig *virtualtools.DelegationTierConfig) *virtualtools.DelegationTierConfig {
	result := &virtualtools.DelegationTierConfig{}
	hasAny := false

	// Start with env var defaults
	if p, m := os.Getenv("DELEGATION_TIER_HIGH_PROVIDER"), os.Getenv("DELEGATION_TIER_HIGH_MODEL"); p != "" && m != "" {
		result.High = &virtualtools.TierModel{Provider: p, ModelID: m}
		hasAny = true
	}
	if p, m := os.Getenv("DELEGATION_TIER_MEDIUM_PROVIDER"), os.Getenv("DELEGATION_TIER_MEDIUM_MODEL"); p != "" && m != "" {
		result.Medium = &virtualtools.TierModel{Provider: p, ModelID: m}
		hasAny = true
	}
	if p, m := os.Getenv("DELEGATION_TIER_LOW_PROVIDER"), os.Getenv("DELEGATION_TIER_LOW_MODEL"); p != "" && m != "" {
		result.Low = &virtualtools.TierModel{Provider: p, ModelID: m}
		hasAny = true
	}

	// Override with frontend config (higher priority)
	if frontendConfig != nil {
		if main := sanitizeTierModel(frontendConfig.Main); main != nil {
			result.Main = main
			hasAny = true
		}
		if high := sanitizeTierModel(frontendConfig.High); high != nil {
			result.High = high
			hasAny = true
		}
		if medium := sanitizeTierModel(frontendConfig.Medium); medium != nil {
			result.Medium = medium
			hasAny = true
		}
		if low := sanitizeTierModel(frontendConfig.Low); low != nil {
			result.Low = low
			hasAny = true
		}
		// Pass through custom tiers from frontend (no env var equivalent)
		if len(frontendConfig.Custom) > 0 {
			result.Custom = frontendConfig.Custom
			hasAny = true
		}
	}

	if !hasAny {
		return nil
	}
	return result
}

// handleGetDelegationTierDefaults returns the env var default values for delegation tier config
func (api *StreamingAPI) handleGetDelegationTierDefaults(w http.ResponseWriter, r *http.Request) {
	tierConfig := resolveDelegationTierConfig(nil) // env vars only
	if tierConfig == nil {
		tierConfig = &virtualtools.DelegationTierConfig{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tierConfig)
}

// getActiveSession retrieves an active session by ID
// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (api *StreamingAPI) getActiveSession(sessionID string) (*ActiveSessionInfo, bool) {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()

	session, exists := api.activeSessions[sessionID]
	return session, exists
}

// getAllActiveSessions returns all active sessions (filtered by activity - 10 minute timeout)
func (api *StreamingAPI) getAllActiveSessions() []*ActiveSessionInfo {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()

	now := time.Now()
	inactivityTimeout := 10 * time.Minute
	recentCompletionWindow := 30 * time.Minute
	sessions := make([]*ActiveSessionInfo, 0, len(api.activeSessions))

	for _, session := range api.activeSessions {
		// Include running sessions that have been active within the last 10 minutes
		if session.Status == "running" && now.Sub(session.LastActivity) < inactivityTimeout {
			sessions = append(sessions, session)
		} else if session.Status == "completed" && now.Sub(session.LastActivity) < recentCompletionWindow {
			// Also include recently completed sessions (within 30 minutes) for page refresh restore
			sessions = append(sessions, session)
		}
	}

	return sessions
}

// cleanupInactiveSessions runs periodically to:
// 1. Mark running sessions as inactive if no events for 10 minutes
// 2. Remove completed/inactive sessions from map after 30 minutes (to prevent memory leaks)
func (api *StreamingAPI) cleanupInactiveSessions() {
	ticker := time.NewTicker(2 * time.Minute) // Check every 2 minutes
	defer ticker.Stop()

	for range ticker.C {
		api.activeSessionsMux.Lock()
		now := time.Now()
		inactivityTimeout := 10 * time.Minute
		completedSessionRetention := 30 * time.Minute
		sessionsToMarkInactive := make([]string, 0)
		sessionsToDelete := make([]string, 0)

		for sessionID, session := range api.activeSessions {
			// Mark as inactive if no activity for 10 minutes and status is still "running"
			if session.Status == "running" && now.Sub(session.LastActivity) >= inactivityTimeout {
				sessionsToMarkInactive = append(sessionsToMarkInactive, sessionID)
			}
			// Delete completed/inactive sessions after 30 minutes to prevent memory leaks
			// These sessions have already been saved to the database
			if (session.Status == "completed" || session.Status == "inactive" || session.Status == "stopped" || session.Status == "error") && now.Sub(session.LastActivity) >= completedSessionRetention {
				sessionsToDelete = append(sessionsToDelete, sessionID)
			}
		}

		// Delete old sessions within the lock
		for _, sessionID := range sessionsToDelete {
			if session, exists := api.activeSessions[sessionID]; exists {
				status := session.Status
				delete(api.activeSessions, sessionID)
				log.Printf("[ACTIVE_SESSION] Cleanup: Removed old %s session %s from memory (>30 min old)", status, sessionID)
			}
		}

		api.activeSessionsMux.Unlock()

		for _, sessionID := range sessionsToDelete {
			if api.eventStore != nil {
				api.eventStore.RemoveSession(sessionID)
				log.Printf("[ACTIVE_SESSION] Cleanup: Removed event buffer for session %s", sessionID)
			}
		}

		// Mark sessions as inactive (outside lock to avoid deadlock)
		for _, sessionID := range sessionsToMarkInactive {
			log.Printf("[ACTIVE_SESSION] Marking session %s as inactive (no activity for 10+ minutes)", sessionID)
			api.updateSessionStatus(sessionID, "inactive")
		}
	}
}

// storeWorkflowOrchestrator stores a workflow orchestrator for a session
func (api *StreamingAPI) storeWorkflowOrchestrator(sessionID string, orchestrator orchestrator.Orchestrator) {
	api.workflowOrchestratorsMux.Lock()
	defer api.workflowOrchestratorsMux.Unlock()
	api.workflowOrchestrators[sessionID] = orchestrator
	log.Printf("[ORCHESTRATOR] Stored workflow orchestrator for session %s", sessionID)
}

// --- LLM GUIDANCE API HANDLERS ---

// deserializeSerializedMessage converts a SerializedMessage (typed) back to llmtypes.MessageContent
//
//nolint:unused // kept for the serialized-history rehydration path during polling refactors.
func deserializeSerializedMessage(serialized unifiedevents.SerializedMessage) *llmtypes.MessageContent {
	var role llmtypes.ChatMessageType
	switch serialized.Role {
	case "human": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeHuman
	case "ai": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeAI
	case "tool": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeTool
	case "system": // Standard value from llmtypes
		role = llmtypes.ChatMessageTypeSystem
	case "user", "assistant": // Fallback for compatibility (shouldn't happen but handle gracefully)
		if serialized.Role == "user" {
			role = llmtypes.ChatMessageTypeHuman
		} else {
			role = llmtypes.ChatMessageTypeAI
		}
	default:
		// Default to human if unknown role
		log.Printf("[DESERIALIZE] Unknown role '%s', defaulting to human", serialized.Role)
		role = llmtypes.ChatMessageTypeHuman
	}

	msg := &llmtypes.MessageContent{
		Role:  role,
		Parts: []llmtypes.ContentPart{},
	}

	for _, part := range serialized.Parts {
		switch part.Type {
		case "text":
			if content, ok := part.Content.(string); ok {
				msg.Parts = append(msg.Parts, llmtypes.TextContent{Text: content})
			}
		case "tool_call":
			if contentMap, ok := part.Content.(map[string]interface{}); ok {
				toolCall := llmtypes.ToolCall{
					FunctionCall: &llmtypes.FunctionCall{}, // Initialize pointer
				}
				if id, ok := contentMap["id"].(string); ok {
					toolCall.ID = id
				}
				if fnName, ok := contentMap["function_name"].(string); ok {
					toolCall.FunctionCall.Name = fnName
				}
				if fnArgs, ok := contentMap["function_args"].(string); ok {
					toolCall.FunctionCall.Arguments = fnArgs
				}
				msg.Parts = append(msg.Parts, toolCall)
			}
		case "tool_response":
			if contentMap, ok := part.Content.(map[string]interface{}); ok {
				toolResp := llmtypes.ToolCallResponse{}
				if toolCallID, ok := contentMap["tool_call_id"].(string); ok {
					toolResp.ToolCallID = toolCallID
				}
				if content, ok := contentMap["content"].(string); ok {
					toolResp.Content = content
				}
				msg.Parts = append(msg.Parts, toolResp)
			}
		}
	}

	return msg
}

// handleSetLLMGuidance sets LLM guidance for a session
func (api *StreamingAPI) handleSetLLMGuidance(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	var req LLMGuidanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate session exists
	api.activeSessionsMux.RLock()
	session, exists := api.activeSessions[sessionID]
	api.activeSessionsMux.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Update guidance in activeSessions
	api.activeSessionsMux.Lock()
	session.LLMGuidance = req.Guidance
	if req.MemoryFolder != "" {
		session.MemoryFolder = req.MemoryFolder
		log.Printf("[LLM_GUIDANCE] Set memory folder for session %s: %s", sessionID, req.MemoryFolder)
	}
	session.LastActivity = time.Now()
	api.activeSessionsMux.Unlock()

	log.Printf("[LLM_GUIDANCE] Set guidance for session %s: %s", sessionID, req.Guidance)

	response := LLMGuidanceResponse{
		SessionID: sessionID,
		Status:    "success",
		Message:   "LLM guidance updated successfully",
		Guidance:  req.Guidance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSteerMessage injects a user message into a running agent's conversation loop.
// The message is picked up after the current tool call completes, before the next LLM call.
func (api *StreamingAPI) handleSteerMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	var req SteerMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Look up the running agent for this session
	api.runningAgentsMux.RLock()
	runningAgent, exists := api.runningAgents[sessionID]
	api.runningAgentsMux.RUnlock()

	if !exists || runningAgent == nil {
		http.Error(w, "No running agent for this session", http.StatusNotFound)
		return
	}

	// Inject the steer message into the agent's pending queue
	runningAgent.AddSteerMessage(req.Message)
	log.Printf("[STEER] Queued steer message for session %s: %.80s", sessionID, req.Message)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Steer message queued for injection",
	})
}

// handleSubmitHumanFeedback handles human feedback submission
func (api *StreamingAPI) handleSubmitHumanFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req HumanFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.UniqueID == "" {
		http.Error(w, "unique_id is required", http.StatusBadRequest)
		return
	}

	if req.Response == "" {
		http.Error(w, "response is required", http.StatusBadRequest)
		return
	}

	// Get human feedback store and submit response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.SubmitResponse(req.UniqueID, req.Response); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[HUMAN_FEEDBACK] Submitted response for unique_id %s: %s", req.UniqueID, req.Response)

	response := HumanFeedbackResponse{
		UniqueID: req.UniqueID,
		Status:   "success",
		Message:  "Human feedback submitted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// buildWorkshopGroupInfo builds a human-readable summary of available variable groups
// for the interactive-workshop system prompt. Includes the user-selected group if any.
func buildWorkshopGroupInfo(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
	selectedRunFolder string,
	enabledGroupNames []string,
) string {
	// Read variables manifest
	varPath := workspacePath + "/variables/variables.json"
	content, err := readFile(ctx, varPath)
	if err != nil {
		return ""
	}

	var manifest todo_creation_human.VariablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}

	if len(manifest.Groups) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%d variable groups** available:\n", len(manifest.Groups)))
	for _, g := range manifest.Groups {
		status := "enabled"
		if !g.Enabled {
			status = "disabled"
		}
		// Mark the user-selected group
		selected := ""
		for _, eid := range enabledGroupNames {
			if eid == g.Name {
				selected = " **[SELECTED]**"
				break
			}
		}
		sb.WriteString(fmt.Sprintf("- **%s** (group_name: `%s`, %s)%s\n", g.Name, g.Name, status, selected))
	}

	if selectedRunFolder != "" {
		sb.WriteString(fmt.Sprintf("\nSelected run folder: `%s`\n", selectedRunFolder))
	}

	if len(enabledGroupNames) > 0 {
		sb.WriteString(fmt.Sprintf("\nUser has selected group(s): %v — use these as default for execute_step calls.\n", enabledGroupNames))
	}

	return sb.String()
}

// buildWorkshopConfig loads the full preset and builds a WorkshopConfig that replicates
// the exact same tool/LLM/browser/image-gen setup as a normal workflow execution.
// This mirrors the logic in the /api/workflow handler (lines ~2003-2260) so the workshop
// gets the same tools, executors, categories, and LLM configs.
func (api *StreamingAPI) buildWorkshopConfig(
	ctx context.Context,
	req QueryRequest,
	currentUserID string,
	workspacePath string,
	runFolder string,
	selectedServers []string,
	sessionID string,
	apiKeys ...*llm.ProviderAPIKeys, // Optional pre-loaded keys (avoids canceled context issues)
) (*todo_creation_human.WorkshopConfig, error) {
	// Extract enabled group names from execution options (toolbar selection)
	var enabledGroupNames []string
	if req.ExecutionOptions != nil && len(req.ExecutionOptions.EnabledGroupNames) > 0 {
		enabledGroupNames = req.ExecutionOptions.EnabledGroupNames
	}

	// Always use merged API keys (env + workspace) for workshop orchestrator
	workshopLLMConfig := req.LLMConfig
	if workshopLLMConfig == nil {
		workshopLLMConfig = &orchestrator.LLMConfig{}
	}
	if len(apiKeys) > 0 && apiKeys[0] != nil {
		workshopLLMConfig.APIKeys = apiKeys[0]
	} else {
		workshopLLMConfig.APIKeys = MergedProviderAPIKeys(ctx)
	}

	cfg := &todo_creation_human.WorkshopConfig{
		WorkspacePath:     workspacePath,
		RunFolder:         runFolder,
		MCPConfigPath:     api.mcpConfigPath,
		SelectedServers:   append([]string(nil), selectedServers...), // copy to avoid mutation
		LLMConfig:         workshopLLMConfig,
		UseKnowledgebase:  true,
		LLMAllocationMode: "manual",
		Logger:            api.logger,
		SessionID:         sessionID,
		EnabledGroupNames: enabledGroupNames,
	}

	// Build base tools with session-aware workspace executors from the start.
	// This ensures MCP_API_URL in shell commands includes the session path prefix
	// (/s/{session_id}/...) so per-tool HTTP calls from inside Docker hit the
	// session-scoped route and get the correct executor.
	allTools, allExecutors, toolCategories := createCustomTools(true, currentUserID, sessionID)

	// Track preset's global secret selection (overrides req.SelectedGlobalSecrets which is nil for phase chat)
	var presetGlobalSecretNames *[]string

	// Load config from workflow.json manifest (single source of truth — no DB dependency).
	// Use context.Background() so a canceled request context doesn't silently skip manifest
	// loading. If the manifest cannot be read, fail immediately — a partially-configured
	// session with missing TieredConfig/servers/tools would cause cryptic failures later.
	if workspacePath != "" {
		manifest, found, mErr := ReadWorkflowManifest(context.Background(), workspacePath)
		if mErr != nil {
			return nil, fmt.Errorf("failed to read workflow manifest from %s: %w", workspacePath, mErr)
		} else if found {
			caps := manifest.Capabilities
			log.Printf("[WORKSHOP] Loaded config from manifest at %s", workspacePath)

			// Manifest is the source of truth for workflow-selected MCP servers.
			// The incoming request can carry stale UI/session server state, which
			// would incorrectly strip step-level servers like playwright during
			// workflow filtering if we keep using it here.
			cfg.SelectedServers = append([]string(nil), caps.SelectedServers...)
			cfg.SelectedTools = caps.SelectedTools
			cfg.UseCodeExecutionMode = caps.UseCodeExecutionMode
			cfg.SelectedSkills = caps.SelectedSkills

			// Global secrets
			if caps.SelectedGlobalSecretNames != nil {
				presetGlobalSecretNames = caps.SelectedGlobalSecretNames
			}

			// Browser mode from manifest capabilities
			effectiveBrowserMode := caps.BrowserMode
			wsHasPlaywright := false
			for _, s := range cfg.SelectedServers {
				if s == "playwright" {
					wsHasPlaywright = true
				}
			}
			if wsHasPlaywright {
				effectiveBrowserMode = "playwright"
				log.Printf("[WORKSHOP] Playwright server detected — using mode=%s", effectiveBrowserMode)
			}
			if effectiveBrowserMode != "" {
				common.SetSessionBrowserMode(sessionID, effectiveBrowserMode)
			}
			if effectiveBrowserMode == "headless" || effectiveBrowserMode == "cdp" {
				cdpPortForBrowser := getCdpPort(req)
				if cdpPortForBrowser == 0 && effectiveBrowserMode == "cdp" {
					cdpPortForBrowser = 9222
				}
				browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()
				browserTools := virtualtools.CreateWorkspaceBrowserTools()
				browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, cdpPortForBrowser)
				allTools = append(allTools, browserTools...)
				for name, executor := range browserExecutors {
					allExecutors[name] = executor
				}
				for _, tool := range browserTools {
					if tool.Function != nil {
						toolCategories[tool.Function.Name] = browserCategory
					}
				}
				log.Printf("[WORKSHOP] Added browser tools (mode=%s, cdp_port=%d)", effectiveBrowserMode, cdpPortForBrowser)

				var filteredServers []string
				for _, s := range cfg.SelectedServers {
					if s != "playwright" {
						filteredServers = append(filteredServers, s)
					}
				}
				if len(filteredServers) != len(cfg.SelectedServers) {
					cfg.SelectedServers = filteredServers
				}
			}

			// LLM config from manifest
			log.Printf("[WORKSHOP] LLMConfig from manifest: isNil=%v", caps.LLMConfig == nil)
			if caps.LLMConfig != nil {
				llmCfg := caps.LLMConfig
				log.Printf("[WORKSHOP] LLMConfig details: allocationMode=%q tieredConfig=%v provider=%q modelID=%q",
					llmCfg.LLMAllocationMode, llmCfg.TieredConfig != nil, llmCfg.Provider, llmCfg.ModelID)
				cfg.PresetPhaseLLM = workshopExtractLLM(llmCfg.PhaseLLM, llmCfg.Provider, llmCfg.ModelID)

				if llmCfg.UseKnowledgebase != nil {
					cfg.UseKnowledgebase = *llmCfg.UseKnowledgebase
				}
				if llmCfg.LockKnowledgebase != nil {
					cfg.LockKnowledgebase = *llmCfg.LockKnowledgebase
				}

				// Tiered LLM allocation
				if llmCfg.LLMAllocationMode == "tiered" && llmCfg.TieredConfig != nil {
					cfg.LLMAllocationMode = "tiered"
					cfg.TieredConfig = workshopConvertTieredLLMConfig(llmCfg.TieredConfig)
					log.Printf("[WORKSHOP] Tiered mode: T1=%s T2=%s T3=%s",
						workshopFormatAgentLLM(cfg.TieredConfig.Tier1),
						workshopFormatAgentLLM(cfg.TieredConfig.Tier2),
						workshopFormatAgentLLM(cfg.TieredConfig.Tier3))
				}

				// Image generation tools
				if llmCfg.EnableImageGeneration != nil && *llmCfg.EnableImageGeneration {
					imgCfg := virtualtools.ImageGenExecutorConfig{
						WorkspaceAPIURL: getWorkspaceAPIURL(),
						UserID:          currentUserID,
					}
					if llmCfg.ImageGenProvider != "" {
						imgCfg.Provider = llmCfg.ImageGenProvider
					}
					if llmCfg.ImageGenModelID != "" {
						imgCfg.ModelID = llmCfg.ImageGenModelID
					}
					virtualtools.MergeImageToolExecutorsUntyped(imgCfg, allExecutors, toolCategories)
					log.Printf("[WORKSHOP] Updated image tool executors (provider=%s model=%s)", imgCfg.Provider, imgCfg.ModelID)
				}

				log.Printf("[WORKSHOP] LLM config loaded: phase=%v tiered=%v kb=%v kbLock=%v",
					cfg.PresetPhaseLLM != nil, cfg.TieredConfig != nil, cfg.UseKnowledgebase, cfg.LockKnowledgebase)
			}
		}
	}

	// Merge secrets — use preset's global secret selection if available (phase chat doesn't send req.SelectedGlobalSecrets)
	effectiveGlobalSecretSelection := req.SelectedGlobalSecrets
	if presetGlobalSecretNames != nil {
		effectiveGlobalSecretSelection = presetGlobalSecretNames
	}
	userSecrets := req.DecryptedSecrets
	if workspacePath != "" {
		if manifest, found, err := ReadWorkflowManifest(context.Background(), workspacePath); err == nil && found {
			userSecrets = api.loadSelectedUserSecrets(context.Background(), currentUserID, manifest.Capabilities.SelectedSecrets)
		}
	}
	allSecrets := mergeGlobalSecrets(userSecrets, effectiveGlobalSecretSelection)
	if len(allSecrets) > 0 {
		entries := make([]orchestrator.SecretEntry, len(allSecrets))
		for i, s := range allSecrets {
			entries[i] = orchestrator.SecretEntry{Name: s.Name, Value: s.Value}
		}
		cfg.Secrets = entries
		log.Printf("[WORKSHOP] Applied %d secrets", len(entries))
	}

	// Replace workspace executors with session-aware versions (same as normal workflow handler).
	// This sets MCP_SESSION_ID and secrets as shell env vars for code execution mode.
	secretEnvVars := make(map[string]string, len(allSecrets))
	for _, s := range allSecrets {
		secretEnvVars["SECRET_"+s.Name] = s.Value
	}
	sessionAwareExecutors, workspaceEnv := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSessionAndEnv(currentUserID, sessionID, secretEnvVars)
	for name, executor := range sessionAwareExecutors {
		allExecutors[name] = executor
	}
	cfg.WorkspaceEnvRef = workspaceEnv
	// Working directory and folder guard are set per-request in handleQuery (line ~4415)
	// via workspace.SetSessionWorkingDir/SetSessionFolderGuard, not here.
	log.Printf("[WORKSHOP] Replaced workspace executors with session-aware versions (sessionID=%q, secrets=%d, MCP_API_URL=%s)", sessionID, len(secretEnvVars), workspaceEnv["MCP_API_URL"])

	cfg.CustomTools = allTools
	cfg.CustomToolExecutors = allExecutors
	cfg.ToolCategories = toolCategories

	// Create workshop event bridge for SSE emission from background goroutines
	cfg.EventBridge = &eventbridge.WorkflowEventBridge{
		BaseEventBridge: &eventbridge.BaseEventBridge{
			EventStore: api.eventStore,
			SessionID:  sessionID,
			Logger:     api.logger,
			BridgeName: "workshop",
		},
	}

	// Wire up live tool call query for query_step_tools
	cfg.ToolCallQueryFunc = formatToolCallSummaries(api)

	// Wire up schedule management callbacks
	// Set workspace path for schedule management — prefer SelectedFolder, fall back to resolving from preset
	if req.SelectedFolder != "" {
		cfg.SchedulerWorkspacePath = req.SelectedFolder
	} else if req.PresetQueryID != "" {
		if wPath, wErr := api.resolveWorkspacePathFromPreset(context.Background(), req.PresetQueryID); wErr == nil && wPath != "" {
			cfg.SchedulerWorkspacePath = wPath
		}
	}
	cfg.SchedulerFuncs = api.buildSchedulerCallbacks()
	cfg.SkillFuncs = api.buildSkillCallbacks()
	cfg.LLMToolsFuncs = api.buildLLMToolsCallbacks()
	cfg.ListAvailableSecrets = func(ctx context.Context) ([]string, error) {
		nameSet := make(map[string]bool)
		// Global secrets from env vars
		for _, gs := range getGlobalSecrets() {
			nameSet[gs.Name] = true
		}
		// User-stored secrets from DB
		userSecrets, err := api.chatStore.ListUserSecrets(ctx, currentUserID)
		if err == nil {
			for _, us := range userSecrets {
				nameSet[us.Name] = true
			}
		}
		names := make([]string, 0, len(nameSet))
		for name := range nameSet {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	cfg.ResolveSecretValues = func(ctx context.Context, names []string) map[string]string {
		if len(names) == 0 {
			return nil
		}
		out := make(map[string]string, len(names))
		wanted := make(map[string]bool, len(names))
		for _, n := range names {
			wanted[n] = true
		}
		// Globals first — they set the baseline. User secrets can override by same name.
		for _, gs := range getGlobalSecrets() {
			if wanted[gs.Name] {
				out[gs.Name] = gs.Value
			}
		}
		decrypted := api.loadSelectedUserSecrets(ctx, currentUserID, names)
		for _, s := range decrypted {
			out[s.Name] = s.Value
		}
		return out
	}

	return cfg, nil
}

// buildSchedulerCallbacks creates SchedulerCallbacks that bridge the workshop tools
// to the workflow.json manifest and scheduler service. No database dependency.
func (api *StreamingAPI) buildSchedulerCallbacks() *todo_creation_human.SchedulerCallbacks {
	return &todo_creation_human.SchedulerCallbacks{
		ListSchedules: func(ctx context.Context, workspacePath string) (string, error) {
			manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
			if err != nil || !found {
				return "No workflow manifest found.", nil
			}
			if len(manifest.Schedules) == 0 {
				return "No schedules found for this workflow.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Schedules (%d found)\n\n", len(manifest.Schedules)))
			for _, sched := range manifest.Schedules {
				status := "disabled"
				if sched.Enabled {
					status = "enabled"
				}
				sb.WriteString(fmt.Sprintf("### %s\n", sched.Name))
				sb.WriteString(fmt.Sprintf("- **ID**: `%s`\n", sched.ID))
				sb.WriteString(fmt.Sprintf("- **Cron**: `%s`\n", sched.CronExpression))
				sb.WriteString(fmt.Sprintf("- **Timezone**: %s\n", sched.Timezone))
				sb.WriteString(fmt.Sprintf("- **Status**: %s\n", status))
				if api.scheduler != nil {
					state := api.scheduler.GetRuntimeState(sched.ID)
					if state.LastStatus != "" {
						sb.WriteString(fmt.Sprintf("- **Last Run**: %v (status: %s)\n", state.LastRunAt, state.LastStatus))
					}
					if state.NextRunAt != nil {
						sb.WriteString(fmt.Sprintf("- **Next Run**: %v\n", state.NextRunAt))
					}
					sb.WriteString(fmt.Sprintf("- **Run Count**: %d\n", state.RunCount))
				}
				if len(sched.GroupNames) > 0 {
					sb.WriteString(fmt.Sprintf("- **Groups**: %v\n", sched.GroupNames))
				} else {
					sb.WriteString("- **Groups**: all\n")
				}
				sb.WriteString("\n")
			}
			return sb.String(), nil
		},
		CreateSchedule: func(ctx context.Context, workspacePath, name, cronExpr, timezone string, groupNames []string, mode string, messages []string, workshopMode string) (string, error) {
			if err := ValidateCronExpression(cronExpr); err != nil {
				return "", fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
			}
			if err := ValidateScheduleTimezone(timezone); err != nil {
				return "", err
			}
			manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
			if err != nil || !found {
				return "", fmt.Errorf("workflow manifest not found at %s", workspacePath)
			}
			groupNames, err = validateScheduleGroupNamesForWorkspace(ctx, workspacePath, groupNames)
			if err != nil {
				return "", err
			}
			newSched := WorkflowSchedule{
				ID:             generateScheduleID(),
				Name:           name,
				CronExpression: cronExpr,
				Timezone:       timezone,
				GroupNames:     groupNames,
				Enabled:        true,
				Mode:           mode,
				Messages:       messages,
				WorkshopMode:   workshopMode,
			}
			manifest.Schedules = append(manifest.Schedules, newSched)
			if err := WriteWorkflowManifest(ctx, workspacePath, manifest); err != nil {
				return "", fmt.Errorf("failed to write manifest: %w", err)
			}
			// Load into gocron scheduler
			if api.scheduler != nil {
				sctx := buildScheduleContext(workspacePath, manifest, newSched)
				if err := api.scheduler.LoadSchedule(sctx); err != nil {
					return fmt.Sprintf("Schedule created (ID: %s) but failed to activate: %v", newSched.ID, err), nil
				}
			}
			nextRun := getNextRunTime(cronExpr, timezone)
			nextRunStr := "unknown"
			if nextRun != nil {
				nextRunStr = nextRun.Format(time.RFC3339)
			}
			return fmt.Sprintf("Schedule created and activated.\n- **ID**: `%s`\n- **Name**: %s\n- **Cron**: `%s`\n- **Timezone**: %s\n- **Next Run**: %s", newSched.ID, name, cronExpr, timezone, nextRunStr), nil
		},
		CreateCalendarSchedule: func(ctx context.Context, workspacePath, name, timezone string, groupNames []string, calendarItemsJSON string, mode string, messages []string, workshopMode string) (string, error) {
			if err := ValidateScheduleTimezone(timezone); err != nil {
				return "", err
			}
			var calendarItems []CalendarScheduleItem
			if err := json.Unmarshal([]byte(calendarItemsJSON), &calendarItems); err != nil {
				return "", fmt.Errorf("invalid calendar_items JSON: %w", err)
			}
			calendarItems = normalizeCalendarScheduleItems(calendarItems)
			if err := validateScheduleRequest("calendar", "", calendarItems); err != nil {
				return "", err
			}
			manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
			if err != nil || !found {
				return "", fmt.Errorf("workflow manifest not found at %s", workspacePath)
			}
			groupNames, err = validateScheduleGroupNamesForWorkspace(ctx, workspacePath, groupNames)
			if err != nil {
				return "", err
			}
			newSched := WorkflowSchedule{
				ID:            generateScheduleID(),
				Name:          name,
				ScheduleType:  "calendar",
				Timezone:      timezone,
				CalendarItems: calendarItems,
				GroupNames:    groupNames,
				Enabled:       true,
				Mode:          mode,
				Messages:      messages,
				WorkshopMode:  workshopMode,
			}
			manifest.Schedules = append(manifest.Schedules, newSched)
			if err := WriteWorkflowManifest(ctx, workspacePath, manifest); err != nil {
				return "", fmt.Errorf("failed to write manifest: %w", err)
			}
			if api.scheduler != nil {
				sctx := buildScheduleContext(workspacePath, manifest, newSched)
				if err := api.scheduler.LoadSchedule(sctx); err != nil {
					return fmt.Sprintf("Calendar schedule created (ID: %s) but failed to activate: %v", newSched.ID, err), nil
				}
			}
			nextRun := getNextRunTimeForCalendar(newSched)
			nextRunStr := "unknown"
			if nextRun != nil {
				nextRunStr = nextRun.Format(time.RFC3339)
			}
			return fmt.Sprintf("Calendar schedule created and activated.\n- **ID**: `%s`\n- **Name**: %s\n- **Items**: %d\n- **Timezone**: %s\n- **Next Run**: %s", newSched.ID, name, len(calendarItems), timezone, nextRunStr), nil
		},
		UpdateSchedule: func(ctx context.Context, jobID, name, cronExpr, timezone string, groupNames []string, setGroupNames bool, enabled *bool, mode string, messages []string, workshopMode string) (string, error) {
			if cronExpr != "" {
				if err := ValidateCronExpression(cronExpr); err != nil {
					return "", fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
				}
			}
			if timezone != "" {
				if err := ValidateScheduleTimezone(timezone); err != nil {
					return "", err
				}
			}
			workspacePath, manifest, idx, err := findScheduleByID(ctx, jobID)
			if err != nil {
				return "", fmt.Errorf("schedule not found: %w", err)
			}
			sched := &manifest.Schedules[idx]
			if name != "" {
				sched.Name = name
			}
			if cronExpr != "" {
				sched.CronExpression = cronExpr
			}
			if timezone != "" {
				sched.Timezone = timezone
			}
			if setGroupNames {
				validGroupNames, err := validateScheduleGroupNamesForWorkspace(ctx, workspacePath, groupNames)
				if err != nil {
					return "", err
				}
				sched.GroupNames = validGroupNames
			}
			if enabled != nil {
				sched.Enabled = *enabled
			}
			if mode != "" {
				sched.Mode = mode
			}
			if messages != nil {
				sched.Messages = messages
			}
			if workshopMode != "" {
				sched.WorkshopMode = workshopMode
			}
			validGroupNames, err := validateScheduleGroupNamesForWorkspace(ctx, workspacePath, sched.GroupNames)
			if err != nil {
				return "", err
			}
			sched.GroupNames = validGroupNames
			if err := WriteWorkflowManifest(ctx, workspacePath, manifest); err != nil {
				return "", fmt.Errorf("failed to write manifest: %w", err)
			}
			if api.scheduler != nil {
				if err := api.scheduler.ReloadSchedule(ctx, workspacePath, jobID); err != nil {
					return fmt.Sprintf("Schedule updated but failed to reload: %v", err), nil
				}
			}
			nextRun := getNextRunTime(sched.CronExpression, sched.Timezone)
			nextRunStr := "unknown"
			if nextRun != nil {
				nextRunStr = nextRun.Format(time.RFC3339)
			}
			return fmt.Sprintf("Schedule updated.\n- **ID**: `%s`\n- **Name**: %s\n- **Cron**: `%s`\n- **Enabled**: %v\n- **Next Run**: %s", sched.ID, sched.Name, sched.CronExpression, sched.Enabled, nextRunStr), nil
		},
		DeleteSchedule: func(ctx context.Context, jobID string) error {
			if api.scheduler != nil {
				api.scheduler.RemoveJob(jobID)
			}
			workspacePath, manifest, idx, err := findScheduleByID(ctx, jobID)
			if err != nil {
				return err
			}
			manifest.Schedules = append(manifest.Schedules[:idx], manifest.Schedules[idx+1:]...)
			return WriteWorkflowManifest(ctx, workspacePath, manifest)
		},
		TriggerSchedule: func(ctx context.Context, jobID string) (string, error) {
			if api.scheduler == nil {
				return "", fmt.Errorf("scheduler not available")
			}
			workspacePath := api.scheduler.GetWorkspaceForSchedule(jobID)
			if workspacePath == "" {
				wp, _, _, err := findScheduleByID(ctx, jobID)
				if err != nil {
					return "", err
				}
				workspacePath = wp
			}
			_, err := api.scheduler.TriggerNow(workspacePath, jobID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Schedule triggered. Job ID: `%s`", jobID), nil
		},
		GetScheduleRuns: func(ctx context.Context, jobID string, limit int) (string, error) {
			if limit <= 0 {
				limit = 10
			}
			workspacePath := ""
			if api.scheduler != nil {
				workspacePath = api.scheduler.GetWorkspaceForSchedule(jobID)
			}
			if workspacePath == "" {
				wp, _, _, err := findScheduleByID(ctx, jobID)
				if err != nil {
					return "", err
				}
				workspacePath = wp
			}
			runs, total, err := ListScheduleRuns(ctx, workspacePath, jobID, limit, 0)
			if err != nil {
				return "", err
			}
			if len(runs) == 0 {
				return "No runs found for this schedule.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Run History (%d of %d)\n\n", len(runs), total))
			for _, r := range runs {
				duration := ""
				if r.DurationMs != nil {
					duration = fmt.Sprintf(" (%dms)", *r.DurationMs)
				}
				idPrefix := r.ID
				if len(idPrefix) > 8 {
					idPrefix = idPrefix[:8]
				}
				sb.WriteString(fmt.Sprintf("- **%s** [%s]%s — %s", idPrefix, r.Status, duration, r.StartedAt.Format("2006-01-02 15:04:05")))
				if r.RunFolder != "" {
					sb.WriteString(fmt.Sprintf(" → `%s`", r.RunFolder))
				}
				if r.Error != "" {
					sb.WriteString(fmt.Sprintf("\n  Error: %s", r.Error))
				}
				sb.WriteString("\n")
			}
			return sb.String(), nil
		},
	}
}

// buildSkillCallbacks creates SkillCallbacks that bridge the workshop tools
// to the workspace skills API. Returns nil-safe callbacks.
func (api *StreamingAPI) buildSkillCallbacks() *todo_creation_human.SkillCallbacks {
	wsURL := getWorkspaceAPIURL() // workspace container URL, not backend URL
	return &todo_creation_human.SkillCallbacks{
		ListSkills: func(ctx context.Context) (string, error) {
			allSkills, err := skills.DiscoverSkills(wsURL)
			if err != nil {
				return "", fmt.Errorf("failed to discover skills: %w", err)
			}
			if len(allSkills) == 0 {
				return "No skills found in the workspace. Use install_skill to add skills.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Skills (%d found)\n\n", len(allSkills)))
			for _, sk := range allSkills {
				sb.WriteString(fmt.Sprintf("### %s\n", sk.Frontmatter.Name))
				sb.WriteString(fmt.Sprintf("- **Folder**: `%s`\n", sk.FolderName))
				if sk.Frontmatter.Description != "" {
					sb.WriteString(fmt.Sprintf("- **Description**: %s\n", sk.Frontmatter.Description))
				}
				if sk.SourceURL != "" {
					sb.WriteString(fmt.Sprintf("- **Source**: %s\n", sk.SourceURL))
				}
				sb.WriteString("\n")
			}
			return sb.String(), nil
		},
		ImportSkill: func(ctx context.Context, githubURL, token string) (string, error) {
			resp, err := skills.ImportGitHubSkill(wsURL, githubURL, token)
			if err != nil {
				return "", fmt.Errorf("failed to import skill: %w", err)
			}
			if !resp.Success {
				return fmt.Sprintf("Failed to import skill: %s", resp.Error), nil
			}
			return fmt.Sprintf("Successfully imported skill **%s**. Use update_workflow_config to add it to the workflow's selected skills.", resp.SkillName), nil
		},
		DeleteSkill: func(ctx context.Context, folderName string) error {
			err := skills.DeleteSkill(wsURL, folderName)
			if err == nil {
				_ = skills.RemoveFromLockFile(wsURL, folderName)
			}
			return err
		},
		SearchSkills: func(ctx context.Context, query string) (string, error) {
			results, err := skills.FindSkills(ctx, query)
			if err != nil {
				return "", fmt.Errorf("failed to search skills: %w", err)
			}
			if len(results) == 0 {
				return "No skills found matching your query.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Search Results (%d found)\n\n", len(results)))
			sb.WriteString("Install with: `install_skill` tool using the source value.\n\n")
			for _, r := range results {
				sb.WriteString(fmt.Sprintf("- **%s** (%s) — %s\n", r.Skill, r.Source, r.Installs))
			}
			return sb.String(), nil
		},
		InstallSkill: func(ctx context.Context, source string) (string, error) {
			result, err := skills.ImportToWorkspace(ctx, wsURL, source)
			if err != nil {
				return "", fmt.Errorf("failed to install skill: %w", err)
			}
			if len(result.InstalledSkills) == 0 {
				return "No skills were installed. Check the source format (e.g., 'owner/repo@skill-name').", nil
			}
			return fmt.Sprintf("Successfully installed: %s. Use update_workflow_config to add to workflow's selected skills.", strings.Join(result.InstalledSkills, ", ")), nil
		},
	}
}

func (api *StreamingAPI) registerMultiAgentSkillTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	skillFuncs := api.buildSkillCallbacks()
	if skillFuncs == nil {
		return fmt.Errorf("skill callbacks unavailable")
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "skill_tools")
	}

	if err := registerTool(
		"list_skills",
		"List skills available in the workspace. Use this to inspect installed skills before selecting them in chat settings or reading their files directly.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return skillFuncs.ListSkills(ctx)
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"import_skill",
		"Import a skill from GitHub into the workspace. Imported skills become available for future chats and can also be read directly from the skills folder.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"github_url": map[string]interface{}{
					"type":        "string",
					"description": "GitHub URL of the skill to import, either a repo URL or a direct path to a skill folder.",
				},
				"token": map[string]interface{}{
					"type":        "string",
					"description": "Optional GitHub personal access token for private repositories.",
				},
			},
			"required": []string{"github_url"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			githubURL, _ := args["github_url"].(string)
			if strings.TrimSpace(githubURL) == "" {
				return "github_url is required.", nil
			}
			token, _ := args["token"].(string)
			return skillFuncs.ImportSkill(ctx, githubURL, token)
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"uninstall_skill",
		"Remove an installed skill from the workspace. Use list_skills first to confirm the folder name.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"folder_name": map[string]interface{}{
					"type":        "string",
					"description": "The skill folder name returned by list_skills.",
				},
			},
			"required": []string{"folder_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			folderName, _ := args["folder_name"].(string)
			if strings.TrimSpace(folderName) == "" {
				return "folder_name is required.", nil
			}
			if err := skillFuncs.DeleteSkill(ctx, folderName); err != nil {
				return fmt.Sprintf("Failed to uninstall skill %q: %v", folderName, err), nil
			}
			return fmt.Sprintf("Successfully uninstalled skill %q from workspace.", folderName), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"search_skills",
		"Search the public skills registry for installable skills. Use install_skill with a returned source value to install one.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search terms such as 'browser automation', 'social media', or 'data analysis'.",
				},
			},
			"required": []string{"query"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			query, _ := args["query"].(string)
			if strings.TrimSpace(query) == "" {
				return "query is required.", nil
			}
			return skillFuncs.SearchSkills(ctx, query)
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"install_skill",
		"Install a skill from the public skills registry using owner/repo@skill-name format. Use search_skills first to find valid sources.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"source": map[string]interface{}{
					"type":        "string",
					"description": "Skill source in owner/repo@skill-name format.",
				},
			},
			"required": []string{"source"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			source, _ := args["source"].(string)
			if strings.TrimSpace(source) == "" {
				return "source is required (e.g. 'owner/repo@skill-name').", nil
			}
			return skillFuncs.InstallSkill(ctx, source)
		},
	); err != nil {
		return err
	}

	return nil
}

// buildLLMToolsCallbacks creates LLMToolsCallbacks that bridge the workshop tools
// to the published LLM list, model metadata catalog, and provider validation logic.
func (api *StreamingAPI) buildLLMToolsCallbacks() *todo_creation_human.LLMToolsCallbacks {
	return &todo_creation_human.LLMToolsCallbacks{
		ListPublishedLLMs: func(ctx context.Context) (string, error) {
			llms, err := LoadPublishedLLMs(ctx)
			if err != nil {
				return "", fmt.Errorf("failed to load published LLMs: %w", err)
			}
			return prettyJSON(map[string]interface{}{
				"count": len(llms),
				"llms":  llms,
				"note":  "These are the published models available for workflow tier configuration.",
			}), nil
		},
		ListProviderModels: func(_ context.Context, provider string) (string, error) {
			return listProviderModelsJSON(provider), nil
		},
		ValidateLLM: func(ctx context.Context, args map[string]interface{}) (string, error) {
			provider := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", args["provider"])))
			modelID, _ := args["model_id"].(string)
			apiKey, _ := args["api_key"].(string)
			endpoint, _ := args["endpoint"].(string)
			region, _ := args["region"].(string)
			apiVersion, _ := args["api_version"].(string)
			options, _ := args["options"].(map[string]interface{})

			if provider == "" {
				return "provider is required.", nil
			}

			validationOptions := cloneOptionsMap(options)
			if raw, ok := args["temperature"].(float64); ok {
				if validationOptions == nil {
					validationOptions = map[string]interface{}{}
				}
				validationOptions["temperature"] = raw
			}

			// Use workspace-backed auth if no explicit key provided
			usedWorkspaceAuth := false
			if strings.TrimSpace(apiKey) == "" {
				keys, err := LoadProviderKeys(ctx)
				if err == nil && keys != nil {
					switch provider {
					case "openrouter":
						if keys.OpenRouter != "" {
							apiKey = keys.OpenRouter
							usedWorkspaceAuth = true
						}
					case "openai":
						if keys.OpenAI != "" {
							apiKey = keys.OpenAI
							usedWorkspaceAuth = true
						}
					case "anthropic":
						if keys.Anthropic != "" {
							apiKey = keys.Anthropic
							usedWorkspaceAuth = true
						}
					case "vertex":
						if keys.Vertex != "" {
							apiKey = keys.Vertex
							usedWorkspaceAuth = true
						}
					case "minimax":
						if keys.MiniMax != "" {
							apiKey = keys.MiniMax
							usedWorkspaceAuth = true
						}
					case "bedrock":
						if keys.Bedrock.Region != "" {
							region = keys.Bedrock.Region
							usedWorkspaceAuth = true
						}
					case "azure":
						if keys.Azure.APIKey != "" {
							apiKey = keys.Azure.APIKey
							usedWorkspaceAuth = true
						}
						if endpoint == "" && keys.Azure.Endpoint != "" {
							endpoint = keys.Azure.Endpoint
						}
						if apiVersion == "" && keys.Azure.APIVersion != "" {
							apiVersion = keys.Azure.APIVersion
						}
					}
				}
			}

			if endpoint != "" || region != "" || apiVersion != "" {
				if validationOptions == nil {
					validationOptions = map[string]interface{}{}
				}
				if endpoint != "" {
					validationOptions["endpoint"] = endpoint
				}
				if region != "" {
					validationOptions["region"] = region
				}
				if apiVersion != "" {
					validationOptions["api_version"] = apiVersion
				}
			}
			req := llm.APIKeyValidationRequest{
				Provider: provider,
				ModelID:  modelID,
				APIKey:   apiKey,
				Options:  validationOptions,
			}

			resp := validateProviderConfig(req)
			return prettyJSON(map[string]interface{}{
				"provider":            provider,
				"model_id":            modelID,
				"valid":               resp.Valid,
				"message":             resp.Message,
				"error":               resp.Error,
				"corrected_options":   resp.CorrectedOptions,
				"used_workspace_auth": usedWorkspaceAuth,
			}), nil
		},
	}
}

func (api *StreamingAPI) registerMultiAgentMCPServerTools(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "mcp_server_tools")
	}

	toStringSlice := func(raw interface{}) []string {
		items, ok := raw.([]interface{})
		if !ok {
			return nil
		}
		result := make([]string, 0, len(items))
		for _, item := range items {
			value, ok := item.(string)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value != "" {
				result = append(result, value)
			}
		}
		return result
	}

	toStringMap := func(raw interface{}) map[string]string {
		items, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}
		result := make(map[string]string, len(items))
		for key, value := range items {
			strValue, ok := value.(string)
			if !ok {
				continue
			}
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			result[trimmedKey] = strValue
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}

	loadUserConfig := func() (string, *mcpclient.MCPConfig, error) {
		userConfigPath := strings.Replace(api.mcpConfigPath, ".json", "_user.json", 1)
		userConfig, err := mcpclient.LoadConfig(userConfigPath, api.logger)
		if err != nil {
			userConfig = &mcpclient.MCPConfig{MCPServers: make(map[string]mcpclient.MCPServerConfig)}
		}
		if userConfig.MCPServers == nil {
			userConfig.MCPServers = make(map[string]mcpclient.MCPServerConfig)
		}
		return userConfigPath, userConfig, nil
	}

	buildServerConfig := func(args map[string]interface{}) (mcpclient.MCPServerConfig, error) {
		server := mcpclient.MCPServerConfig{
			Args:       toStringSlice(args["args"]),
			Env:        toStringMap(args["env"]),
			Headers:    toStringMap(args["headers"]),
			PoolConfig: nil,
		}
		if value, ok := args["command"].(string); ok {
			server.Command = strings.TrimSpace(value)
		}
		if value, ok := args["working_dir"].(string); ok {
			server.WorkingDir = strings.TrimSpace(value)
		}
		if value, ok := args["description"].(string); ok {
			server.Description = strings.TrimSpace(value)
		}
		if value, ok := args["url"].(string); ok {
			server.URL = strings.TrimSpace(value)
		}
		if value, ok := args["protocol"].(string); ok {
			server.Protocol = mcpclient.ProtocolType(strings.TrimSpace(value))
		}
		return server, nil
	}

	if err := registerTool(
		"list_mcp_servers",
		"List configured MCP servers, including whether they come from the base config or user config and whether discovery has succeeded.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			mergedConfig, err := api.loadMergedConfig()
			if err != nil {
				return "", fmt.Errorf("failed to load MCP config: %w", err)
			}

			userConfigPath, userConfig, err := loadUserConfig()
			if err != nil {
				return "", err
			}

			api.toolStatusMux.RLock()
			toolStatusCopy := make(map[string]ToolStatus, len(api.toolStatus))
			for name, status := range api.toolStatus {
				toolStatusCopy[name] = status
			}
			api.toolStatusMux.RUnlock()

			names := make([]string, 0, len(mergedConfig.MCPServers))
			for name := range mergedConfig.MCPServers {
				names = append(names, name)
			}
			sort.Strings(names)

			var sb strings.Builder
			sb.WriteString("## MCP Servers\n\n")
			if len(names) == 0 {
				sb.WriteString("No MCP servers are configured.\n")
				return sb.String(), nil
			}

			if isMCPConfigLocked() {
				sb.WriteString("Configuration mode: locked (read-only)\n\n")
			} else {
				sb.WriteString(fmt.Sprintf("User config path: `%s`\n\n", userConfigPath))
			}

			for _, name := range names {
				server := mergedConfig.MCPServers[name]
				source := "base"
				if _, ok := userConfig.MCPServers[name]; ok {
					source = "user"
				}

				statusLabel := "not yet discovered"
				if status, ok := toolStatusCopy[name]; ok {
					switch status.Status {
					case "ok":
						statusLabel = fmt.Sprintf("discovered (%d tools)", len(status.FunctionNames))
					case "error":
						statusLabel = "discovery failed"
					default:
						statusLabel = status.Status
					}
				}

				sb.WriteString(fmt.Sprintf("- `%s` [%s] [%s]\n", name, source, statusLabel))
				if server.Description != "" {
					sb.WriteString(fmt.Sprintf("  %s\n", server.Description))
				}
				protocol := server.GetProtocol()
				if server.URL != "" {
					sb.WriteString(fmt.Sprintf("  protocol: `%s`, url: `%s`\n", protocol, server.URL))
				} else {
					sb.WriteString(fmt.Sprintf("  protocol: `%s`, command: `%s`\n", protocol, server.Command))
				}
				if status, ok := toolStatusCopy[name]; ok && status.Error != "" {
					sb.WriteString(fmt.Sprintf("  last error: %s\n", status.Error))
				}
			}

			return sb.String(), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"add_mcp_server",
		"Add a new user-defined MCP server configuration, then trigger discovery. This does not modify admin/base servers.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Unique server name.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional human-readable description.",
				},
				"protocol": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"stdio", "sse", "http"},
					"description": "Optional explicit protocol. If omitted, the backend infers it from url or command when possible.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Command to run for stdio servers.",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Command arguments for stdio servers.",
				},
				"env": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "string"},
					"description":          "Optional environment variables for stdio servers.",
				},
				"working_dir": map[string]interface{}{
					"type":        "string",
					"description": "Optional working directory for stdio servers.",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL for SSE or HTTP MCP servers.",
				},
				"headers": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "string"},
					"description":          "Optional HTTP headers for SSE or HTTP MCP servers.",
				},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if isMCPConfigLocked() {
				return "MCP configuration is locked by the administrator, so chat cannot add or update servers.", nil
			}

			name, _ := args["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "name is required.", nil
			}

			if _, exists := api.mcpConfig.MCPServers[name]; exists {
				return fmt.Sprintf("Server %q is part of the base/admin config and can't be modified from chat. Use a new name or update the base config directly.", name), nil
			}

			userConfigPath, userConfig, err := loadUserConfig()
			if err != nil {
				return "", err
			}
			if _, exists := userConfig.MCPServers[name]; exists {
				return fmt.Sprintf("User-defined MCP server %q already exists. Use edit_mcp_server to change it.", name), nil
			}

			server, err := buildServerConfig(args)
			if err != nil {
				return "", err
			}

			if err := api.validateMCPConfig(&mcpclient.MCPConfig{
				MCPServers: map[string]mcpclient.MCPServerConfig{name: server},
			}); err != nil {
				return fmt.Sprintf("Invalid MCP server config: %v", err), nil
			}

			userConfig.MCPServers[name] = server
			if err := mcpclient.SaveConfig(userConfigPath, userConfig); err != nil {
				return "", fmt.Errorf("failed to save user MCP config: %w", err)
			}

			api.appendServerLog(name, "info", "Server configuration saved from multi-agent chat, triggering discovery...")
			go api.triggerMCPDiscovery()

			return fmt.Sprintf("Saved user MCP server %q and started discovery. It will be available to future chats and sessions after discovery completes.", name), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"edit_mcp_server",
		"Edit an existing user-defined MCP server configuration, then trigger discovery. Base/admin servers cannot be edited from chat.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Existing user-defined server name.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional human-readable description.",
				},
				"protocol": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"stdio", "sse", "http"},
					"description": "Optional explicit protocol. If omitted, the backend infers it from url or command when possible.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Command to run for stdio servers.",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Command arguments for stdio servers.",
				},
				"env": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "string"},
					"description":          "Optional environment variables for stdio servers.",
				},
				"working_dir": map[string]interface{}{
					"type":        "string",
					"description": "Optional working directory for stdio servers.",
				},
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL for SSE or HTTP MCP servers.",
				},
				"headers": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{"type": "string"},
					"description":          "Optional HTTP headers for SSE or HTTP MCP servers.",
				},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if isMCPConfigLocked() {
				return "MCP configuration is locked by the administrator, so chat cannot edit servers.", nil
			}

			name, _ := args["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "name is required.", nil
			}

			if _, exists := api.mcpConfig.MCPServers[name]; exists {
				return fmt.Sprintf("Server %q is part of the base/admin config and can't be edited from chat.", name), nil
			}

			userConfigPath, userConfig, err := loadUserConfig()
			if err != nil {
				return "", err
			}
			if _, exists := userConfig.MCPServers[name]; !exists {
				return fmt.Sprintf("User-defined MCP server %q does not exist. Use add_mcp_server first.", name), nil
			}

			server, err := buildServerConfig(args)
			if err != nil {
				return "", err
			}
			if err := api.validateMCPConfig(&mcpclient.MCPConfig{
				MCPServers: map[string]mcpclient.MCPServerConfig{name: server},
			}); err != nil {
				return fmt.Sprintf("Invalid MCP server config: %v", err), nil
			}

			userConfig.MCPServers[name] = server
			if err := mcpclient.SaveConfig(userConfigPath, userConfig); err != nil {
				return "", fmt.Errorf("failed to save user MCP config: %w", err)
			}

			api.appendServerLog(name, "info", "Server configuration edited from multi-agent chat, triggering discovery...")
			go api.triggerMCPDiscovery()

			return fmt.Sprintf("Updated user MCP server %q and started discovery refresh.", name), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"remove_mcp_server",
		"Remove a user-defined MCP server configuration. Base/admin servers cannot be removed from chat.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Server name to remove.",
				},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			if isMCPConfigLocked() {
				return "MCP configuration is locked by the administrator, so chat cannot remove servers.", nil
			}

			name, _ := args["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "name is required.", nil
			}

			if _, exists := api.mcpConfig.MCPServers[name]; exists {
				return fmt.Sprintf("Server %q is part of the base/admin config and can't be removed from chat.", name), nil
			}

			userConfigPath, userConfig, err := loadUserConfig()
			if err != nil {
				return "", err
			}
			if _, exists := userConfig.MCPServers[name]; !exists {
				return fmt.Sprintf("User-defined MCP server %q was not found.", name), nil
			}

			delete(userConfig.MCPServers, name)
			if err := mcpclient.SaveConfig(userConfigPath, userConfig); err != nil {
				return "", fmt.Errorf("failed to save user MCP config: %w", err)
			}

			api.appendServerLog(name, "info", "Server removed from user MCP config")
			go api.triggerMCPDiscovery()

			return fmt.Sprintf("Removed user MCP server %q and started discovery refresh.", name), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"get_mcp_server_logs",
		"Show recent logs for a specific MCP server, or list which servers currently have logs.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Optional server name. If omitted, the tool lists servers that currently have stored logs.",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			name, _ := args["name"].(string)
			name = strings.TrimSpace(name)

			api.serverLogsMux.RLock()
			defer api.serverLogsMux.RUnlock()

			if name == "" {
				if len(api.serverLogs) == 0 {
					return "No MCP server logs are currently stored.", nil
				}
				names := make([]string, 0, len(api.serverLogs))
				for serverName := range api.serverLogs {
					names = append(names, serverName)
				}
				sort.Strings(names)
				return fmt.Sprintf("Servers with stored MCP logs: %s", strings.Join(names, ", ")), nil
			}

			logs := api.serverLogs[name]
			if len(logs) == 0 {
				return fmt.Sprintf("No stored logs for MCP server %q.", name), nil
			}

			start := 0
			if len(logs) > 20 {
				start = len(logs) - 20
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## MCP Server Logs: %s\n\n", name))
			for _, entry := range logs[start:] {
				sb.WriteString(fmt.Sprintf("- %s [%s] %s\n", entry.Timestamp.Format(time.RFC3339), entry.Level, entry.Message))
			}
			return sb.String(), nil
		},
	); err != nil {
		return err
	}

	if err := registerTool(
		"trigger_mcp_discovery",
		"Trigger MCP server discovery in the background after config changes or when you want to refresh server tool metadata.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			go api.triggerMCPDiscovery()
			return "Triggered MCP discovery in the background.", nil
		},
	); err != nil {
		return err
	}

	return nil
}

// truncateString truncates s to maxLen and appends "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// formatToolCallSummaries returns a ToolCallQueryFunc that formats event store tool calls.
// When toolCallID is empty, returns a summary with truncated args/results.
// When toolCallID is set, returns full input/output for that specific call.
func formatToolCallSummaries(api *StreamingAPI) todo_creation_human.ToolCallQueryFunc {
	return func(mainSessID, correlationID, toolCallID string) string {
		summaries := api.eventStore.GetToolCallsByCorrelation(mainSessID, correlationID)
		if len(summaries) == 0 {
			return ""
		}

		// Detailed mode: find specific tool call and return full args/result
		if toolCallID != "" {
			for _, tc := range summaries {
				if tc.ToolCallID == toolCallID {
					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("**%s** [%s]", tc.ToolName, strings.ToUpper(tc.Status)))
					if tc.Duration > 0 {
						sb.WriteString(fmt.Sprintf(" (%s)", tc.Duration.Round(time.Millisecond)))
					}
					sb.WriteString(fmt.Sprintf("\ntool_call_id: %s", tc.ToolCallID))
					if tc.Args != "" {
						sb.WriteString(fmt.Sprintf("\n\n**Input:**\n```json\n%s\n```", tc.Args))
					}
					if tc.Result != "" {
						sb.WriteString(fmt.Sprintf("\n\n**Output:**\n```\n%s\n```", tc.Result))
					}
					return sb.String()
				}
			}
			return fmt.Sprintf("tool_call_id %q not found", toolCallID)
		}

		// Summary mode: truncated args/results
		var sb strings.Builder
		for i, tc := range summaries {
			if i > 0 {
				sb.WriteString("\n")
			}
			switch tc.Status {
			case "running":
				sb.WriteString(fmt.Sprintf("- [RUNNING] %s (id: %s)", tc.ToolName, tc.ToolCallID))
			case "done":
				sb.WriteString(fmt.Sprintf("- [DONE] %s (%s) (id: %s)", tc.ToolName, tc.Duration.Round(time.Millisecond), tc.ToolCallID))
			case "error":
				sb.WriteString(fmt.Sprintf("- [ERROR] %s (%s) (id: %s)", tc.ToolName, tc.Duration.Round(time.Millisecond), tc.ToolCallID))
			}
			if tc.Args != "" {
				sb.WriteString(fmt.Sprintf("\n  Args: %s", truncateString(tc.Args, 200)))
			}
			if tc.Result != "" {
				sb.WriteString(fmt.Sprintf("\n  Result: %s", truncateString(tc.Result, 200)))
			}
		}
		return sb.String()
	}
}

// workshopExtractLLM extracts an AgentLLMConfig from preset config, with legacy fallback.
// Returns nil if neither specific nor legacy values are set.
func workshopExtractLLM(specific *workflowtypes.AgentLLMConfig, legacyProvider, legacyModelID string) *todo_creation_human.AgentLLMConfig {
	if specific != nil && specific.Provider != "" && specific.ModelID != "" {
		return &todo_creation_human.AgentLLMConfig{
			Provider:  specific.Provider,
			ModelID:   specific.ModelID,
			Fallbacks: workshopConvertFallbacks(specific.Fallbacks),
		}
	}
	if legacyProvider != "" && legacyModelID != "" {
		return &todo_creation_human.AgentLLMConfig{
			Provider: legacyProvider,
			ModelID:  legacyModelID,
		}
	}
	return nil
}

func workshopConvertAgentLLMConfig(config *workflowtypes.AgentLLMConfig) *todo_creation_human.AgentLLMConfig {
	if config == nil {
		return nil
	}
	return &todo_creation_human.AgentLLMConfig{
		Provider:  config.Provider,
		ModelID:   config.ModelID,
		Fallbacks: workshopConvertFallbacks(config.Fallbacks),
	}
}

func workshopConvertTieredLLMConfig(config *workflowtypes.TieredLLMConfig) *todo_creation_human.TieredLLMConfig {
	if config == nil {
		return nil
	}

	tiered := &todo_creation_human.TieredLLMConfig{
		Tier1: workshopConvertAgentLLMConfig(config.Tier1),
		Tier2: workshopConvertAgentLLMConfig(config.Tier2),
		Tier3: workshopConvertAgentLLMConfig(config.Tier3),
	}

	if tiered.Tier1 == nil || tiered.Tier2 == nil || tiered.Tier3 == nil {
		log.Printf("[WORKSHOP] Partial tiered LLM config detected: T1=%t T2=%t T3=%t",
			tiered.Tier1 != nil, tiered.Tier2 != nil, tiered.Tier3 != nil)
	}

	return tiered
}

func workshopFormatAgentLLM(config *todo_creation_human.AgentLLMConfig) string {
	if config == nil {
		return "<nil>"
	}
	if config.Provider == "" && config.ModelID == "" {
		return "<empty>"
	}
	return fmt.Sprintf("%s/%s", config.Provider, config.ModelID)
}

// workshopConvertFallbacks converts database fallbacks to step_based_workflow fallbacks.
func workshopConvertFallbacks(fallbacks []workflowtypes.AgentLLMFallback) []todo_creation_human.AgentLLMFallback {
	if len(fallbacks) == 0 {
		return nil
	}
	result := make([]todo_creation_human.AgentLLMFallback, len(fallbacks))
	for i, fb := range fallbacks {
		result[i] = todo_creation_human.AgentLLMFallback{
			Provider: fb.Provider,
			ModelID:  fb.ModelID,
		}
	}
	return result
}
