package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" // Register pprof handlers
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"mcp-agent-builder-go/agent_go/internal/events"
	agent "mcp-agent-builder-go/agent_go/pkg/agentwrapper"
	"mcp-agent-builder-go/agent_go/pkg/database"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	orchEvents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	orchtypes "mcp-agent-builder-go/agent_go/pkg/orchestrator/types"

	unifiedevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/logger"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/subagents"

	"github.com/joho/godotenv"

	eventbridge "mcp-agent-builder-go/agent_go/cmd/server/event_bridge"
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
	SessionID       string    `json:"session_id"`
	AgentMode       string    `json:"agent_mode"`
	Status          string    `json:"status"` // "running", "paused", "completed"
	LastActivity    time.Time `json:"last_activity"`
	CreatedAt       time.Time `json:"created_at"`
	Query           string    `json:"query,omitempty"`
	LLMGuidance     string    `json:"llm_guidance,omitempty"`  // LLM guidance message for this session
	MemoryFolder    string    `json:"memory_folder,omitempty"` // Override memory folder (default: Plans/memories)
	UserID          string    `json:"-"`                       // User ID for session isolation (not exposed in JSON)
	IsSyntheticTurn bool      `json:"is_synthetic_turn"`       // True when running an auto-notification turn (not user-initiated)
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

	// Database for chat history storage
	chatDB database.Database

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

	// Per-session plan phase tracking for multi-agent mode
	planSessionStates    map[string]*virtualtools.PlanSessionState
	planSessionStatesMux sync.RWMutex

	// Session reactivation lock: prevents race conditions when calculating baseIndex
	// and initializing the event store for reactivated sessions
	sessionReactivationMux sync.Mutex

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
	lastQueryRequests map[string]QueryRequest
	lastQueryMu       sync.RWMutex

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
	// Tool search mode: When enabled, LLM discovers tools on-demand via search_tools
	UseToolSearchMode bool `json:"use_tool_search_mode,omitempty"`
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
	// Workspace access configuration
	EnableWorkspaceAccess *bool `json:"enable_workspace_access,omitempty"` // Enable/disable workspace file access tools (nil = inherit default, true/false = explicit override)
	// Browser automation access configuration
	EnableBrowserAccess *bool `json:"enable_browser_access,omitempty"` // Enable/disable browser automation tool (nil = inherit default, true/false = explicit override)
	// Explicit browser mode from frontend: none|headless|cdp|playwright|stealth
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
	// Selected sub-agent templates to make available for delegation
	SelectedSubAgents []string `json:"selected_subagents,omitempty"` // Array of sub-agent template folder names
	// Delegation mode: 'spawn' = simple delegate only, 'plan' = plan-driven + delegate, '' = disabled
	DelegationMode string `json:"delegation_mode,omitempty"`
	// Plan phase override: 'planning' = plan first (default), 'execution' = skip planning and execute directly
	PlanPhase string `json:"plan_phase,omitempty"`
	// Existing plan folder to reuse (pre-seeds PlanSessionState so LLM reuses it)
	PlanFolder string `json:"plan_folder,omitempty"`
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
	case "none", "headless", "cdp", "playwright", "stealth":
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
		if s == "camofox" {
			return "stealth"
		}
	}
	for _, s := range req.EnabledServers {
		if s == "playwright" {
			return "playwright"
		}
	}
	return "none"
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
		case "camofox":
			cfg.HasCamofox = true
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
	ServerCmd.Flags().Int("max-turns", 100, "Maximum conversation turns")
	ServerCmd.Flags().String("mcp-config", "configs/mcp_servers_clean.json", "MCP servers configuration path")

	// Chat History Database flags
	ServerCmd.Flags().String("db-path", "/app/chat_history.db", "SQLite database path for chat history")
	ServerCmd.Flags().String("db-type", "sqlite", "Database type (sqlite, postgres)")

	// Bind flags to viper
	viper.BindPFlags(ServerCmd.Flags())
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

	// Initialize polling system (activity callback will be set after api is created)
	eventStore := events.NewEventStore(10000) // Max 10000 events per session

	// Initialize chat history database
	dbType := viper.GetString("db-type")
	if dbType == "" {
		dbType = "sqlite"
	}

	var chatDB database.Database
	var connInfo string

	if dbType == "postgres" {
		connStr := os.Getenv("DATABASE_URL")
		if connStr == "" {
			log.Fatalf("DATABASE_URL environment variable is required for postgres")
		}
		chatDB, err = database.NewSupabaseDB(connStr)
		connInfo = "PostgreSQL (Supabase)"
	} else {
		dbPath := os.Getenv("DB_PATH")
		if dbPath == "" {
			dbPath = viper.GetString("db-path")
		}
		if dbPath == "" {
			dbPath = "/app/chat_history.db" // Default SQLite database path
		}
		chatDB, err = database.NewSQLiteDB(dbPath)
		connInfo = fmt.Sprintf("SQLite (%s)", dbPath)
	}

	if err != nil {
		log.Printf("⚠️  Failed to initialize chat history database: %v", err)
		if dbType == "sqlite" {
			// SQLite may fail on network storage (Azure Files/SMB) which doesn't support
			// POSIX file locking. Fall back to local ephemeral storage.
			fallbackPath := "/tmp/chat_history.db"
			log.Printf("⚠️  Retrying with local ephemeral storage: %s", fallbackPath)
			chatDB, err = database.NewSQLiteDB(fallbackPath)
			if err != nil {
				log.Fatalf("Failed to initialize chat history database after fallback: %v", err)
			}
			connInfo = fmt.Sprintf("SQLite (%s) [fallback from network storage]", fallbackPath)
		} else {
			log.Fatalf("Failed to initialize chat history database: %v", err)
		}
	}
	defer chatDB.Close()

	fmt.Printf("💾 Chat History Database: %s\n", connInfo)

	// Initialize Slack service for human feedback
	slackSvc, err := slackservice.InitSlackService(chatDB.GetDB())
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
		sessionQueryIDs:              make(map[string][]string),
		workflowObjectives:           make(map[string]string),
		conversationHistory:          make(map[string][]llmtypes.MessageContent),
		chatDB:                       chatDB,
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
		// Initialize plan session state tracking for multi-agent mode
		planSessionStates: make(map[string]*virtualtools.PlanSessionState),
		// Initialize orchestrator storage
		workflowOrchestrators: make(map[string]orchestrator.Orchestrator),
		// Initialize workflow step ID storage
		workflowStepIDs: make(map[string]string),
		// Initialize background agent infrastructure
		bgAgentRegistry:       NewBackgroundAgentRegistry(),
		sessionBusy:           make(map[string]bool),
		pendingCompletions:    make(map[string][]string),
		lastQueryRequests:     make(map[string]QueryRequest),
		sessionAgents:         make(map[string]*agent.LLMAgentWrapper),
		runningAgents:         make(map[string]*mcpagent.Agent),
		completionLoopStarted: make(map[string]bool),
		claudeCodeSessionIDs:  make(map[string]string),
		geminiSessionIDs:      make(map[string]string),
		geminiProjectDirIDs:   make(map[string]string),
	}

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

	// Authentication API routes (public - no auth required, handled by AuthMiddleware)
	apiRouter.HandleFunc("/auth/register", api.handleRegister).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/login", api.handleLogin).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/logout", api.handleLogout).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/me", api.handleGetCurrentUser).Methods("GET")
	apiRouter.HandleFunc("/auth/mode", api.handleGetAuthMode).Methods("GET")
	// Multi-provider OAuth routes
	apiRouter.HandleFunc("/auth/start", api.handleAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/callback", api.handleAuthCallback).Methods("GET")

	// Session sharing routes
	apiRouter.HandleFunc("/sessions/{session_id}/share", api.handleCreateShare).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/sessions/{session_id}/shares", api.handleListShares).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/share/{share_id}", api.handleRevokeShare).Methods("DELETE", "OPTIONS")

	// Public shared session access (no auth required)
	apiRouter.HandleFunc("/shared/{share_token}", api.handleGetSharedSession).Methods("GET")
	apiRouter.HandleFunc("/query", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/chat", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/chat/stream", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/health", api.handleHealth).Methods("GET")
	apiRouter.HandleFunc("/capabilities", api.handleCapabilities).Methods("GET")
	apiRouter.HandleFunc("/cdp-check", api.handleCdpCheck).Methods("GET")
	apiRouter.HandleFunc("/camofox-start", api.handleCamofoxStart).Methods("POST")
	apiRouter.HandleFunc("/downloads/chrome-cdp-macOS.zip", api.handleChromeCdpDownload).Methods("GET")
	apiRouter.HandleFunc("/llm-config/defaults", api.handleGetLLMDefaults).Methods("GET")
	apiRouter.HandleFunc("/llm-config/models/metadata", api.handleGetModelMetadata).Methods("GET")
	apiRouter.HandleFunc("/llm-config/azure/deployments", api.handleGetAzureDeployedModels).Methods("POST")
	apiRouter.HandleFunc("/llm-config/validate-key", api.handleValidateAPIKey).Methods("POST")
	apiRouter.HandleFunc("/image-gen/test", api.handleTestImageGen).Methods("POST")
	apiRouter.HandleFunc("/llm-config/delegation-tiers", api.handleGetDelegationTierDefaults).Methods("GET")
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
	workspaceAbsPath, wpErr := filepath.Abs("../workspace-docs/_users/default")
	if wpErr != nil {
		log.Printf("[BROWSER_UPLOAD] Warning: failed to resolve workspace-docs abs path: %v", wpErr)
	} else {
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
				// Join with absolute workspace path to produce host-resolvable path
				resolved := filepath.Join(workspaceAbsPath, pathStr)
				log.Printf("[BROWSER_UPLOAD] Resolved path[%d]: %q -> %q", i, pathStr, resolved)
				paths[i] = resolved
			}
		})
	}

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
		"workspace_advanced": true, "workspace_git": true, "workspace_image_gen": true,
		"workspace_image_edit": true, "human": true,
		"workflow": true,
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
		r.Header.Set("X-Session-ID", vars["session_id"])
		routeMCPRequest(w, r, vars["server"], vars["tool"])
	}).Methods("POST", "OPTIONS")
	sessionToolsRouter.HandleFunc("/custom/{tool}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sid := vars["session_id"]
		r.Header.Set("X-Session-ID", sid)
		// Inject ChatSessionIDKey so execute_shell_command can look up
		// the session's working directory and folder guard from the global map.
		ctx := context.WithValue(r.Context(), common.ChatSessionIDKey, sid)
		executorHandlers.HandlePerToolCustomRequest(w, r.WithContext(ctx), vars["tool"])
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

	// OAuth API routes (from oauth_routes.go)
	apiRouter.HandleFunc("/oauth/start", api.handleOAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/oauth/callback", api.handleOAuthCallback).Methods("GET")
	apiRouter.HandleFunc("/oauth/status", api.handleOAuthStatus).Methods("GET")
	apiRouter.HandleFunc("/oauth/logout", api.handleOAuthLogout).Methods("POST", "OPTIONS")

	// Observer APIs removed - events are now stored by sessionID, no observers needed

	// Active Session API routes (from polling.go)
	apiRouter.HandleFunc("/sessions/active", api.handleGetActiveSessions).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events", api.handleGetSessionEvents).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events/stream", api.handleSSEStream).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/reconnect", api.handleReconnectSession).Methods("POST")
	apiRouter.HandleFunc("/sessions/{session_id}/status", api.handleGetSessionStatus).Methods("GET")
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

	// Chat History API routes
	apiRouter.HandleFunc("/chat-history/sessions", createChatSessionHandler(chatDB)).Methods("POST")
	apiRouter.HandleFunc("/chat-history/sessions", listChatSessionsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}", getChatSessionHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}", updateChatSessionHandler(chatDB)).Methods("PUT")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}", deleteChatSessionHandler(chatDB)).Methods("DELETE")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/events", getSessionEventsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/events", searchEventsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/health", chatHistoryHealthCheckHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/costs", getAllSessionCostsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/costs", getSessionCostsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/delegation-logs", getAllDelegationLogsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/delegation-logs", getDelegationLogsHandler(chatDB)).Methods("GET")
	apiRouter.HandleFunc("/chat-history/sessions/{session_id}/delegation-logs/{delegation_id}/events", getDelegationEventsHandler(chatDB)).Methods("GET")

	// Preset Queries API routes
	PresetQueryRoutes(router, chatDB)

	// Slack Feedback API routes
	SlackFeedbackRoutes(router, api, chatDB)

	// Initialize Bot Conversation Manager
	workspaceURL := os.Getenv("WORKSPACE_API_URL")
	if workspaceURL == "" {
		workspaceURL = "http://localhost:8081"
	}
	botManager := slackservice.NewBotConversationManager(chatDB, configPath, workspaceURL)
	botManager.SetEventSubscriber(NewBotEventSubscriberAdapter(eventStore))
	// Bot sessions use ONLY delegation tier config from DB for LLM selection — no server defaults needed
	api.botManager = botManager
	// Wire startSessionInternal after api is created (closure captures api)
	botManager.SetStartSessionFunc(api.startSessionInternal)
	botManager.SetFollowUpFunc(api.sendFollowUpInternal)
	botManager.SetUserSecretsLoader(func(ctx context.Context, userID string) ([]slackservice.DecryptedSecret, error) {
		stored, err := chatDB.ListUserSecrets(ctx, userID)
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
		botConfig, _ := chatDB.GetBotConnectorConfig(context.Background(), "slack")
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

	// Register bot routes
	BotRoutes(router, api, chatDB)
	BotSimulatorRoutes(router, api, chatDB)

	// Set activity callback for event store to update session LastActivity when events are added
	eventStore.SetActivityCallback(func(sessionID string) {
		api.updateSessionActivity(sessionID)
	})

	// Start background cleanup goroutine to mark inactive sessions (10 minute timeout)
	go api.cleanupInactiveSessions()

	// Initialize and start the cron scheduler
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	schedulerSvc := NewSchedulerService(chatDB, api)
	api.scheduler = schedulerSvc
	go func() {
		if err := schedulerSvc.Start(schedulerCtx); err != nil {
			log.Printf("[SCHEDULER] Error: %v", err)
		}
	}()

	// Register scheduler routes
	SchedulerRoutes(router, chatDB, schedulerSvc)

	// Workflow API routes
	apiRouter.HandleFunc("/workflow/create", api.handleCreateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/status", api.handleGetWorkflowStatus).Methods("GET")
	apiRouter.HandleFunc("/workflow/update", api.handleUpdateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/constants", orchtypes.HandleWorkflowConstants).Methods("GET")
	apiRouter.HandleFunc("/workflow/active-executions", api.handleGetActiveExecutions).Methods("GET", "OPTIONS")

	// Employee API routes (in employee_routes.go)
	EmployeeRoutes(router, chatDB)

	// Consolidated workspace state endpoint (NEW - loads everything in one call)
	apiRouter.HandleFunc("/workspace/state", api.handleLoadWorkspaceState).Methods("GET", "OPTIONS")

	// Legacy individual endpoints (kept for backward compatibility)
	apiRouter.HandleFunc("/workflow/run-folders", api.handleGetRunFolders).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/run-folder", api.handleCreateRunFolder).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/progress", api.handleGetProgress).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/run-folder", api.handleDeleteRunFolder).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflow/learnings", api.handleDeleteStepLearnings).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/workflow/learnings/all", api.handleGetAllStepLearnings).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/variable-groups", api.handleGetVariableGroups).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/variable-groups", api.handleUpdateVariableGroups).Methods("POST", "PUT", "OPTIONS")
	apiRouter.HandleFunc("/workflow/logs", api.handleGetExecutionLogs).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/logs/file", api.handleGetLogFile).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/costs", api.handleGetCosts).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/evaluation-reports", api.handleGetEvaluationReports).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/final-outputs", api.handleGetFinalOutputs).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/final-outputs/generate", api.handleGenerateFinalOutput).Methods("POST", "OPTIONS")

	// Plan and Step Config API routes
	apiRouter.HandleFunc("/workflow/plan/update-step", api.handleUpdatePlanStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/update-step-config", api.handleUpdateStepConfig).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/batch-update-steps", api.handleBatchUpdateSteps).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/delete-step", api.handleDeleteStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/add-step", api.handleAddStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/step-override", api.handleGetStepOverride).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/step-override", api.handleUpdateStepOverride).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/final-output", api.handleGetFinalOutputConfig).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/final-output", api.handleUpdateFinalOutputConfig).Methods("POST", "OPTIONS")

	// Workflow Version API routes
	apiRouter.HandleFunc("/workflow/versions", api.handleListVersions).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/workflow/versions/publish", api.handlePublishVersion).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/versions/revert", api.handleRevertVersion).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/versions", api.handleDeleteVersion).Methods("DELETE", "OPTIONS")

	// Skills API routes (from skill_routes.go)
	RegisterSkillRoutes(apiRouter, api)

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

	// Static file serving (for frontend)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		WriteTimeout: 0,                 // No write timeout — long-running tool calls (sub-agents) can take 30+ minutes
		ReadTimeout:  time.Second * 30,  // Read timeout for incoming requests
		IdleTimeout:  time.Second * 300, // 5 min idle timeout to prevent early closes during long queries
		Handler:      router,
	}

	// Initialize tool cache BEFORE starting HTTP server so the first getTools()
	// request from the frontend gets real data instead of an empty list.
	fmt.Printf("🔄 Initializing tool cache on server startup...\n")
	api.initializeToolCache()

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	fmt.Printf("✅ Server started on %s:%d\n", config.Host, config.Port)
	fmt.Printf("🔗 API endpoint: http://%s:%d/api/query\n", config.Host, config.Port)
	fmt.Printf("📡 Polling API: http://%s:%d/api/sessions/{session_id}/events\n", config.Host, config.Port)

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

// GetCodeExecAPIURL returns the API URL as seen from inside the workspace container.
// Shell commands in code execution mode run inside the Docker workspace-api container,
// so they need host.docker.internal to reach the Go server on the host.
// Falls back to the normal API URL when workspace is not Dockerized.
func (api *StreamingAPI) GetCodeExecAPIURL() string {
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

// getOrCreatePlanSessionState returns the session-level plan state, creating one if needed.
// On first access for a session, it restores plan state from the database if available,
// allowing resumed sessions to pick up existing plans.
func (api *StreamingAPI) getOrCreatePlanSessionState(sessionID string) *virtualtools.PlanSessionState {
	api.planSessionStatesMux.Lock()
	defer api.planSessionStatesMux.Unlock()
	if state, exists := api.planSessionStates[sessionID]; exists {
		return state
	}
	state := virtualtools.NewPlanSessionState()

	// Try to restore plan state from database session config
	if api.chatDB != nil {
		if chatSession, err := api.chatDB.GetChatSession(context.Background(), sessionID); err == nil && chatSession != nil {
			if config, err := chatSession.GetConfig(); err == nil && config != nil && config.PlanID != "" {
				state.PlanID = config.PlanID
				state.PlanFolder = config.PlanFolder
				if config.PlanPhase != "" {
					state.Phase = config.PlanPhase
				}
				log.Printf("[PLAN STATE] Restored plan state from DB for session %s: plan=%s folder=%s phase=%s",
					sessionID, config.PlanID, config.PlanFolder, state.Phase)
			}
		}
	}

	api.planSessionStates[sessionID] = state
	return state
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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
		"agent_modes": []string{"simple", "orchestrator", "workflow"},
		"tracing": map[string]interface{}{
			"enabled":  tracingProvider != "noop",
			"provider": tracingProvider,
		},
		"workspace": map[string]interface{}{
			"semantic_search_enabled": workspace.IsSemanticSearchEnabled(),
			"github_sync_enabled":     workspace.IsGitSyncEnabled(),
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

// handleCamofoxStart checks if camofox-browser is running, starts it if not, and returns status.
// Called by the frontend when the user selects Stealth Browser mode.
func (api *StreamingAPI) handleCamofoxStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse request body for headed preference
	var reqBody struct {
		Headed *bool `json:"headed"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
	}
	headed := true // default to headed (visible browser)
	if reqBody.Headed != nil {
		headed = *reqBody.Headed
	}

	port := 9377
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", port)

	// Check if already running
	if api.camofoxHealthCheck(healthURL) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": true,
			"started":   false,
			"message":   "camofox-browser already running",
		})
		return
	}

	// Not running — start it
	headlessEnv := "CAMOFOX_HEADLESS=false"
	if !headed {
		headlessEnv = "CAMOFOX_HEADLESS=true"
	}
	log.Printf("[CAMOFOX] Starting camofox-browser on port %d (headed=%v)...", port, headed)
	cmd := exec.Command("camofox-browser")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", port),
		headlessEnv,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Printf("[CAMOFOX] Failed to start camofox-browser: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"started":   false,
			"error":     fmt.Sprintf("failed to start camofox-browser: %v", err),
		})
		return
	}

	// Detach — don't wait for the process
	go func() {
		_ = cmd.Wait()
	}()

	// Poll health endpoint for up to 20 seconds
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if api.camofoxHealthCheck(healthURL) {
			log.Printf("[CAMOFOX] camofox-browser is ready (pid %d)", cmd.Process.Pid)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"connected": true,
				"started":   true,
				"message":   "camofox-browser started successfully",
			})
			return
		}
	}

	log.Printf("[CAMOFOX] camofox-browser did not become ready within 20s")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected": false,
		"started":   true,
		"error":     "camofox-browser started but did not become ready within 20 seconds",
	})
}

// camofoxHealthCheck hits the camofox-browser /health endpoint
func (api *StreamingAPI) camofoxHealthCheck(healthURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
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

	// Default maxTurns from environment variable or 100 if not provided or 0 (applies to both workflow and simple agent modes)
	if req.MaxTurns <= 0 {
		req.MaxTurns = orchestrator.GetDefaultMaxTurnsFromEnv()
		log.Printf("[AGENT] MaxTurns not provided or 0, defaulting to %d (from env or 100)", req.MaxTurns)
	}

	// Use enabled_servers if provided, otherwise fall back to servers
	selectedServers := req.EnabledServers
	if len(selectedServers) == 0 {
		selectedServers = req.Servers
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
	log.Printf("[USER_ID_DEBUGGING] HTTP handler: currentUserID=%q (from auth context)", currentUserID)

	// Create or get chat session for this query
	// The agent will modify the session ID to agent-init-{sessionID}-{timestamp}
	// So we need to create the chat session with the original sessionID
	// and the events will use the modified sessionID
	chatSession, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
	if err != nil {
		// Chat session doesn't exist, create a new one
		// Extract title from req.Query (user message)
		// Remove file context suffix if present (format: "...\n\n📁 Files in context: ...")
		title := req.Query
		if idx := strings.Index(title, "\n\n📁 Files in context:"); idx != -1 {
			title = title[:idx]
		}
		title = strings.TrimSpace(title)
		// Truncate to 50 characters
		if len(title) > 50 {
			title = title[:50] + "..."
		}

		// Build typed config from request
		var configJSON json.RawMessage
		hasConfig := len(req.Servers) > 0 || len(req.EnabledServers) > 0 || req.UseCodeExecutionMode || req.EnableContextSummarization != nil || req.Provider != "" || req.ModelID != "" || req.LLMConfig != nil || len(req.SelectedSkills) > 0 || len(req.SelectedSubAgents) > 0 || req.DelegationMode != "" || req.AgentMode == "workflow" || req.AgentMode == "workflow_phase" || req.AgentMode == "organization_chat"
		if hasConfig {
			config := &database.ChatSessionConfig{
				SelectedServers:      req.Servers,
				EnabledServers:       req.EnabledServers,
				UseCodeExecutionMode: req.UseCodeExecutionMode,
				SelectedSkills:       req.SelectedSkills,
				SelectedSubAgents:    req.SelectedSubAgents,
				EnableContextSummarization: func() *bool {
					if req.EnableContextSummarization != nil {
						val := *req.EnableContextSummarization
						return &val
					}
					return nil
				}(),
			}

			// Extract LLM config (prefer LLMConfig field, fallback to Provider/ModelID)
			if req.LLMConfig != nil {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.LLMConfig.Primary.Provider,
					ModelID:  req.LLMConfig.Primary.ModelID,
				}
				// Convert Fallbacks to FallbackModels for storage
				if len(req.LLMConfig.Fallbacks) > 0 {
					for _, fallback := range req.LLMConfig.Fallbacks {
						config.LLMConfig.FallbackModels = append(config.LLMConfig.FallbackModels, fallback.ModelID)
					}
				}
			} else if req.Provider != "" || req.ModelID != "" {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.Provider,
					ModelID:  req.ModelID,
				}
			}

			// Store delegation mode and tier config for session persistence
			if req.DelegationMode != "" {
				config.DelegationMode = req.DelegationMode
			}
			if req.DelegationTierConfig != nil {
				tierJSON, marshalErr := json.Marshal(req.DelegationTierConfig)
				if marshalErr == nil {
					config.DelegationTierConfig = tierJSON
				}
			}

			// Populate workflow metadata for workflow sessions (enables restore after refresh)
			if (req.AgentMode == "workflow" || req.AgentMode == "workflow_phase") && req.PresetQueryID != "" {
				// Look up preset label/folder for metadata
				presetCtx, presetCancel := context.WithTimeout(context.Background(), 5*time.Second)
				presetForMeta, presetErr := api.chatDB.GetPresetQuery(presetCtx, req.PresetQueryID)
				presetCancel()
				wfMeta := &database.WorkflowMetadata{
					PresetID: req.PresetQueryID,
				}
				if presetErr == nil && presetForMeta != nil {
					wfMeta.PresetName = presetForMeta.Label
					if presetForMeta.SelectedFolder.Valid {
						wfMeta.WorkspacePath = presetForMeta.SelectedFolder.String
					}
				}
				if req.PhaseID != "" {
					wfMeta.PhaseID = req.PhaseID
					wfMeta.PhaseName = req.PhaseID // Will be updated by frontend later
				}
				config.WorkflowMetadata = wfMeta
			}

			// If LLM config is locked and not provided by frontend, use server defaults from env
			if config.LLMConfig == nil && isGlobalLLMConfigLocked() {
				provider, modelID := getPrimaryProviderAndModelFromDefaults()
				if provider != "" && modelID != "" {
					config.LLMConfig = &database.LLMConfigForStorage{
						Provider: provider,
						ModelID:  modelID,
					}
					log.Printf("[CONFIG DEBUG] Using locked LLM config from env: provider=%s, model=%s", provider, modelID)
				}
			}

			var err error
			configJSON, err = config.ToJSON()
			if err != nil {
				log.Printf("[CONFIG DEBUG] Failed to marshal config: %v", err)
				configJSON = nil
			}
		}

		// Ensure preset_query_id is set for workflow sessions
		presetQueryID := req.PresetQueryID
		if presetQueryID == "" && (req.AgentMode == "workflow" || req.AgentMode == "workflow_phase") {
			log.Printf("[SESSION CREATE] WARNING: No preset_query_id for workflow session %s (mode=%s, query=%s)", sessionID, req.AgentMode, title)
		}

		chatSession, err = api.chatDB.CreateChatSessionWithUser(r.Context(), &database.CreateChatSessionRequest{
			SessionID:     sessionID,
			Title:         title,
			AgentMode:     req.AgentMode,
			PresetQueryID: presetQueryID,
			Config:        configJSON,
		}, currentUserID)
		if err != nil {
			log.Printf("[DATABASE DEBUG] Failed to create chat session: %v", err)
			// Continue without chat session - events won't be stored but query can proceed
		}
	} else {
		// Prepare update request
		updateReq := &database.UpdateChatSessionRequest{}
		shouldUpdate := false

		// 1. Update PresetQueryID if provided and currently missing or different
		// This fixes "orphan" sessions by associating them with the current preset
		if req.PresetQueryID != "" {
			currentID := ""
			if chatSession.PresetQueryID != nil {
				currentID = *chatSession.PresetQueryID
			}
			if currentID != req.PresetQueryID {
				updateReq.PresetQueryID = req.PresetQueryID
				shouldUpdate = true
				log.Printf("[SESSION UPDATE] Updating session %s PresetQueryID from '%s' to '%s'", sessionID, currentID, req.PresetQueryID)
			}
		}

		// 2. Reactivate session if it was completed/error (but NOT stopped - user-stopped sessions should not auto-resume)
		if chatSession.Status == "completed" || chatSession.Status == "error" {
			updateReq.Status = "active"
			updateReq.CompletedAt = nil // Clear completion timestamp when reactivating
			shouldUpdate = true
			log.Printf("[SESSION REACTIVATION] Reactivating session %s (old status: %s)", sessionID, chatSession.Status)
		}

		// 3. Update config if skills or other settings changed
		// This ensures selected_skills and other settings are persisted on each query
		hasConfigToUpdate := len(req.SelectedSkills) > 0 || len(req.SelectedSubAgents) > 0 || len(req.EnabledServers) > 0 || req.UseCodeExecutionMode || req.LLMConfig != nil || req.DelegationMode != ""
		if hasConfigToUpdate {
			config := &database.ChatSessionConfig{
				SelectedServers:      req.Servers,
				EnabledServers:       req.EnabledServers,
				UseCodeExecutionMode: req.UseCodeExecutionMode,
				SelectedSkills:       req.SelectedSkills,
				SelectedSubAgents:    req.SelectedSubAgents,
				EnableContextSummarization: func() *bool {
					if req.EnableContextSummarization != nil {
						val := *req.EnableContextSummarization
						return &val
					}
					return nil
				}(),
			}

			// Extract LLM config
			if req.LLMConfig != nil {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.LLMConfig.Primary.Provider,
					ModelID:  req.LLMConfig.Primary.ModelID,
				}
				if len(req.LLMConfig.Fallbacks) > 0 {
					for _, fallback := range req.LLMConfig.Fallbacks {
						config.LLMConfig.FallbackModels = append(config.LLMConfig.FallbackModels, fallback.ModelID)
					}
				}
			} else if req.Provider != "" || req.ModelID != "" {
				config.LLMConfig = &database.LLMConfigForStorage{
					Provider: req.Provider,
					ModelID:  req.ModelID,
				}
			}

			// Preserve delegation mode and tier config on session updates
			if req.DelegationMode != "" {
				config.DelegationMode = req.DelegationMode
			}
			if req.DelegationTierConfig != nil {
				tierJSON, marshalErr := json.Marshal(req.DelegationTierConfig)
				if marshalErr == nil {
					config.DelegationTierConfig = tierJSON
				}
			}

			configJSON, err := config.ToJSON()
			if err != nil {
				log.Printf("[CONFIG DEBUG] Failed to marshal config for update: %v", err)
			} else {
				updateReq.Config = configJSON
				shouldUpdate = true
				log.Printf("[SESSION UPDATE] Updating session %s config with selected_skills=%v", sessionID, req.SelectedSkills)
			}
		}

		// Apply updates if needed
		if shouldUpdate {
			_, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, updateReq)
			if err != nil {
				log.Printf("[DATABASE ERROR] Failed to update chat session %s: %v", sessionID, err)
				// Continue with existing session state - critical path should not fail
			}
		}

		// Initialize EventStore for reactivated session to ensure new events are stored correctly
		// Only needed if we actually reactivated (status changed)
		if chatSession.Status == "stopped" || chatSession.Status == "completed" || chatSession.Status == "error" {
			// CRITICAL: Lock session reactivation to prevent race conditions
			// Multiple concurrent requests could calculate different baseIndex values
			// and initialize the session with misaligned event indices
			api.sessionReactivationMux.Lock()

			// Calculate existing event count to use as baseIndex for polling
			// Use COUNT query instead of fetching all events — much faster for large sessions
			var baseIndex int
			countQuery := "SELECT COUNT(*) FROM events WHERE chat_session_id = ?"
			if isPostgresDB(api.chatDB) {
				countQuery = "SELECT COUNT(*) FROM events WHERE chat_session_id = $1"
			}
			err := api.chatDB.GetDB().QueryRowContext(r.Context(), countQuery, chatSession.ID).Scan(&baseIndex)
			if err != nil {
				log.Printf("[SESSION REACTIVATION] Failed to count existing events for session %s: %v", sessionID, err)
				baseIndex = 0
			} else {
				log.Printf("[SESSION REACTIVATION] Found %d existing events for session %s, setting baseIndex", baseIndex, sessionID)
			}
			api.eventStore.InitializeSession(sessionID, baseIndex)

			api.sessionReactivationMux.Unlock()
		}
	}

	// Track active session for page refresh recovery (no observer needed)
	api.trackActiveSession(sessionID, req.AgentMode, req.Query, currentUserID)
	enableBrowserAccess := req.EnableBrowserAccess != nil && *req.EnableBrowserAccess
	cdpPort := 0
	if req.CdpPort != nil {
		cdpPort = *req.CdpPort
	}
	log.Printf(
		"[QUERY] delegation_mode=%q plan_phase=%q session=%s enable_browser_access=%v browser_mode=%q cdp_port=%d enabled_servers=%v llm_guidance_len=%d query=%q",
		req.DelegationMode,
		req.PlanPhase,
		sessionID,
		enableBrowserAccess,
		getBrowserMode(req),
		cdpPort,
		req.EnabledServers,
		len(req.LLMGuidance),
		req.Query,
	)
	log.Printf("[LATENCY_DEBUG] T+%dms | Session setup complete | sessionID=%s", time.Since(startTime).Milliseconds(), sessionID)

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

	// If provider is still empty (e.g. follow-up message with no config), load from stored session config
	if finalProvider == "" && chatSession != nil && len(chatSession.Config) > 0 {
		var storedConfig database.ChatSessionConfig
		if err := json.Unmarshal(chatSession.Config, &storedConfig); err == nil && storedConfig.LLMConfig != nil {
			if storedConfig.LLMConfig.Provider != "" {
				finalProvider = storedConfig.LLMConfig.Provider
				finalModelID = storedConfig.LLMConfig.ModelID
				log.Printf("[SESSION FALLBACK] Loaded provider/model from stored session config: %s/%s", finalProvider, finalModelID)
			}
		}
	}

	// Handle workflow phase chat mode - convert to simple agent with phase-specific prompt + tools
	// This runs BEFORE the workflow orchestrator branch to intercept and redirect
	isWorkflowPhase := req.AgentMode == "workflow_phase"
	isOrganizationChat := req.AgentMode == "organization_chat"
	workflowPhaseID := req.PhaseID
	workflowPhaseFolder := "" // The preset's SelectedFolder — used to auto-grant write access in FolderGuard
	_ = workflowPhaseFolder   // used later in the function
	if isWorkflowPhase {
		log.Printf("[WORKFLOW_PHASE] Phase chat mode detected: phase=%s preset=%s session=%s", workflowPhaseID, req.PresetQueryID, sessionID)
		if req.PresetQueryID == "" {
			log.Printf("[WORKFLOW_PHASE] ERROR: workflow_phase mode requires a preset_query_id")
			http.Error(w, `{"error":"workflow_phase mode requires a preset_query_id (workflow preset)"}`, http.StatusBadRequest)
			return
		}
		phaseCtx, phaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		phasePreset, phasePresetErr := api.chatDB.GetPresetQuery(phaseCtx, req.PresetQueryID)
		phaseCancel()
		if phasePresetErr == nil && phasePreset != nil {
			// Override LLM with phase-specific LLM from preset
			if len(phasePreset.LLMConfig) > 0 {
				var phaseLLMConfig database.PresetLLMConfig
				if err := json.Unmarshal(phasePreset.LLMConfig, &phaseLLMConfig); err == nil && phaseLLMConfig.PhaseLLM != nil {
					finalProvider = phaseLLMConfig.PhaseLLM.Provider
					finalModelID = phaseLLMConfig.PhaseLLM.ModelID
					log.Printf("[WORKFLOW_PHASE] Using phase LLM from preset: %s/%s", finalProvider, finalModelID)
				}
			}
			// Resolve workflow folder for FolderGuard — ensures workflow-builder
			// always has write access to its own Workflow/X folder
			if phasePreset.SelectedFolder.Valid && phasePreset.SelectedFolder.String != "" {
				workflowPhaseFolder = phasePreset.SelectedFolder.String
				log.Printf("[WORKFLOW_PHASE] Resolved workflow folder for FolderGuard: %s", workflowPhaseFolder)
			}
			// Load global secret selection from preset — frontend may not send selected_global_secrets
			// for workflow_phase requests. Without this, nil selection means ALL global secrets leak.
			if req.SelectedGlobalSecrets == nil {
				if phasePreset.SelectedGlobalSecretNames != "" {
					var names []string
					if err := json.Unmarshal([]byte(phasePreset.SelectedGlobalSecretNames), &names); err != nil {
						log.Printf("[WORKFLOW_PHASE] Failed to parse selected_global_secret_names from preset: %v", err)
					} else {
						req.SelectedGlobalSecrets = &names
						log.Printf("[WORKFLOW_PHASE] Loaded %d selected global secret names from preset", len(names))
					}
				} else {
					emptyNames := []string{}
					req.SelectedGlobalSecrets = &emptyNames
					log.Printf("[WORKFLOW_PHASE] No global secrets configured in preset — defaulting to none")
				}
			}
		}
		// Convert to simple agent mode so it falls through to the standard agent path
		req.AgentMode = "simple"
	}

	// Handle workflow mode - use workflow orchestrator
	if req.AgentMode == "workflow" {

		// Check if preset_id is provided and workflow is approved
		if req.PresetQueryID != "" {
			log.Printf("[WORKFLOW CHECK] Checking workflow approval status for preset_id: %s", req.PresetQueryID)

			// Get workflow from database to check approval status
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			workflow, err := api.chatDB.GetWorkflowByPresetQueryID(ctx, req.PresetQueryID)
			if err != nil {
				log.Printf("[WORKFLOW CHECK ERROR] Workflow not found for preset_id %s: %v", req.PresetQueryID, err)
				// Continue with planning phase if workflow not found
			} else {
				log.Printf("[WORKFLOW CHECK] Found workflow: workflowStatus=%s", workflow.WorkflowStatus)

				// If workflow is approved, proceed with execution using user's query
				if workflow.WorkflowStatus == database.WorkflowStatusPostVerification {
					log.Printf("[WORKFLOW CHECK] Workflow is approved - proceeding with execution using user query: %s", req.Query)
				} else {
					log.Printf("[WORKFLOW CHECK] Workflow is not approved yet - proceeding with planning phase")
				}
			}
		}

		// Create workflow event bridge for event emission
		// Note: ChatDB is set to nil - workflow execution events are stored in memory only (for polling API)
		// Chat history database storage is disabled for workflow execution to reduce database load
		// (workflow_phase / builder sessions use the standard chat path which already persists to DB)
		workflowEventBridge := &eventbridge.WorkflowEventBridge{
			BaseEventBridge: &eventbridge.BaseEventBridge{
				EventStore: api.eventStore,
				SessionID:  sessionID,
				Logger:     api.logger,
				ChatDB:     nil, // Workflow execution only — builder sessions already persist via "simple" agent path
				BridgeName: "workflow",
			},
		}

		// Create custom tools for workflow agents (workspace tools + human tools)
		// Workflow agents can be Simple or ReAct agents, tools are registered based on mode
		// TODO: Memory tools removed from workflow - only needed for individual React agents
		// memoryTools := virtualtools.CreateMemoryTools()
		// memoryExecutors := virtualtools.CreateMemoryToolExecutors()
		allTools, allExecutors, toolCategories := createCustomTools(true) // Workflow mode: all tools (basic + git + advanced + human)

		// NOTE: Workspace executor replacement with session + secrets happens after secrets are merged (see below).

		// Load selected tools, code execution mode, tool search mode, skills, and preset LLM config from preset if available (for workflow agents)
		var selectedTools []string
		var useCodeExecutionMode bool
		var useToolSearchMode bool
		var preDiscoveredTools []string
		var selectedSkills []string
		var presetLLMConfig *database.PresetLLMConfig
		if req.PresetQueryID != "" {
			ctx := context.Background()
			preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
			if err == nil {
				// Load selected tools
				if preset.SelectedTools != "" {
					if err := json.Unmarshal([]byte(preset.SelectedTools), &selectedTools); err != nil {
						log.Printf("[TOOLS] Failed to parse selected tools from preset: %v", err)
					} else {
						if len(selectedTools) > 0 {
							log.Printf("[TOOLS] Loaded %d specific tools from preset", len(selectedTools))
						} else {
							log.Printf("[TOOLS] Preset has empty tool selection - will use ALL tools from selected servers")
						}
					}
				}
				// Load preset LLM config for agent defaults
				if len(preset.LLMConfig) > 0 {
					log.Printf("[PRESET LLM DEBUG] Raw preset LLM config JSON: %s", string(preset.LLMConfig))
					if err := json.Unmarshal(preset.LLMConfig, &presetLLMConfig); err != nil {
						log.Printf("[PRESET LLM] Failed to parse preset LLM config: %v", err)
					} else {
						log.Printf("[PRESET LLM] Loaded preset LLM config with agent defaults")
						// Debug: log what was loaded
						if presetLLMConfig != nil {
							log.Printf("[PRESET LLM DEBUG] Legacy: provider=%s, model=%s", presetLLMConfig.Provider, presetLLMConfig.ModelID)
							if presetLLMConfig.LearningLLM != nil {
								log.Printf("[PRESET LLM DEBUG] LearningLLM: provider=%s, model=%s", presetLLMConfig.LearningLLM.Provider, presetLLMConfig.LearningLLM.ModelID)
							}
							if presetLLMConfig.PhaseLLM != nil {
								log.Printf("[PRESET LLM DEBUG] PhaseLLM: provider=%s, model=%s", presetLLMConfig.PhaseLLM.Provider, presetLLMConfig.PhaseLLM.ModelID)
							} else {
								log.Printf("[PRESET LLM DEBUG] PhaseLLM: nil (will use fallback)")
							}
						} else {
							log.Printf("[PRESET LLM DEBUG] presetLLMConfig is nil after unmarshal")
						}
					}
				} else {
					log.Printf("[PRESET LLM DEBUG] No preset LLM config found (empty or null)")
				}
				// Load code execution mode from preset
				useCodeExecutionMode = preset.UseCodeExecutionMode
				if useCodeExecutionMode {
					log.Printf("[CODE_EXECUTION] Code execution mode enabled from preset")
				}
				// Load tool search mode from preset
				useToolSearchMode = preset.UseToolSearchMode
				if useToolSearchMode {
					log.Printf("[TOOL_SEARCH] Tool search mode enabled from preset")
				}
				// Load pre-discovered tools from preset
				if preset.PreDiscoveredTools != "" {
					if err := json.Unmarshal([]byte(preset.PreDiscoveredTools), &preDiscoveredTools); err != nil {
						log.Printf("[TOOL_SEARCH] Failed to parse pre-discovered tools from preset: %v", err)
					} else if len(preDiscoveredTools) > 0 {
						log.Printf("[TOOL_SEARCH] Loaded %d pre-discovered tools from preset", len(preDiscoveredTools))
					}
				}
				// Load selected skills from preset
				if preset.SelectedSkills != "" {
					var skills []string
					if err := json.Unmarshal([]byte(preset.SelectedSkills), &skills); err != nil {
						log.Printf("[SKILLS] Failed to parse selected skills from preset: %v", err)
					} else if len(skills) > 0 {
						selectedSkills = skills
						log.Printf("[SKILLS] Loaded %d skills from preset: %v", len(skills), skills)
					}
				}
				// Load global secret selection from preset — override nil req.SelectedGlobalSecrets
				// which the frontend doesn't send for workflow mode.
				// NULL (never configured) defaults to NO secrets — global secrets must be explicitly selected.
				if preset.SelectedGlobalSecretNames != "" {
					var names []string
					if err := json.Unmarshal([]byte(preset.SelectedGlobalSecretNames), &names); err != nil {
						log.Printf("[SECRETS] Failed to parse selected_global_secret_names from preset: %v", err)
					} else {
						req.SelectedGlobalSecrets = &names
						log.Printf("[SECRETS] Loaded %d selected global secret names from preset", len(names))
					}
				} else if req.SelectedGlobalSecrets == nil {
					// Preset never configured global secrets — default to none, not all.
					emptyNames := []string{}
					req.SelectedGlobalSecrets = &emptyNames
					log.Printf("[SECRETS] No global secrets configured in preset — defaulting to none")
				}

				// Load browser access mode from preset
				// Resolve effective browser mode: prefer BrowserMode, fall back to EnableBrowserAccess
				workflowBrowserMode := preset.BrowserMode
				if workflowBrowserMode == "" && preset.EnableBrowserAccess {
					workflowBrowserMode = "headless"
				}
				if workflowBrowserMode == "headless" || workflowBrowserMode == "cdp" {
					// Resolve CDP port: prefer request, fall back to preset browser_mode
					wfCdpPort := getCdpPort(req)
					if wfCdpPort == 0 && workflowBrowserMode == "cdp" {
						wfCdpPort = 9222
					}

					// Add browser tools to the available tools pool
					browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()
					browserTools := virtualtools.CreateWorkspaceBrowserTools()
					browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, wfCdpPort)

					allTools = append(allTools, browserTools...)
					for name, executor := range browserExecutors {
						allExecutors[name] = executor
					}

					// Assign category to browser tools
					for _, tool := range browserTools {
						if tool.Function != nil {
							toolCategories[tool.Function.Name] = browserCategory
						}
					}
					log.Printf("[WORKFLOW] Added browser tools (mode=%s, cdp_port=%d, sessionID=%s)", workflowBrowserMode, wfCdpPort, sessionID)

					// Auto-add agent-browser skill if not already selected
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

					// Headless/CDP mode uses agent_browser virtual tool, not playwright/camofox MCP servers.
					// Strip these MCP servers to prevent the agent from discovering and using their tools
					// instead of the intended headless browser.
					var filteredServers []string
					for _, s := range selectedServers {
						if s != "playwright" && s != "camofox" {
							filteredServers = append(filteredServers, s)
						}
					}
					if len(filteredServers) != len(selectedServers) {
						log.Printf("[WORKFLOW] Headless browser mode: stripped playwright/camofox MCP servers from server list (was %d, now %d)", len(selectedServers), len(filteredServers))
						selectedServers = filteredServers
						// Update serverList as well
						if len(selectedServers) == 0 {
							serverList = mcpclient.NoServers
						} else {
							serverList = strings.Join(selectedServers, ",")
						}
					}
				}

				// Load image generation from preset LLM config
				if presetLLMConfig != nil && presetLLMConfig.EnableImageGeneration != nil && *presetLLMConfig.EnableImageGeneration {
					imgCfg := virtualtools.ImageGenExecutorConfig{
						Provider:        "vertex",
						ModelID:         "gemini-2.5-flash-image",
						WorkspaceAPIURL: getWorkspaceAPIURL(),
						UserID:          currentUserID,
					}
					if presetLLMConfig.ImageGenProvider != "" {
						imgCfg.Provider = presetLLMConfig.ImageGenProvider
					}
					if presetLLMConfig.ImageGenModelID != "" {
						imgCfg.ModelID = presetLLMConfig.ImageGenModelID
					}
					for _, def := range []struct {
						tool     func() llmtypes.Tool
						executor func(virtualtools.ImageGenExecutorConfig) func(context.Context, map[string]any) (string, error)
						category func() string
					}{
						{virtualtools.GetImageGenToolDefinition, virtualtools.CreateImageGenExecutor, virtualtools.GetImageGenToolCategory},
						{virtualtools.GetImageEditToolDefinition, virtualtools.CreateImageEditExecutor, virtualtools.GetImageEditToolCategory},
					} {
						t := def.tool()
						exec := def.executor(imgCfg)
						allTools = append(allTools, t)
						allExecutors[t.Function.Name] = exec
						toolCategories[t.Function.Name] = def.category()
						log.Printf("[WORKFLOW] Registered image gen tool: %s (provider=%s model=%s)", t.Function.Name, imgCfg.Provider, imgCfg.ModelID)
					}
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

		// Use code execution mode from request if preset didn't provide any
		if !useCodeExecutionMode && req.UseCodeExecutionMode {
			useCodeExecutionMode = req.UseCodeExecutionMode
			log.Printf("[CODE_EXECUTION] Code execution mode enabled from request")
		}

		// NOTE: Code execution mode for claude-code/gemini-cli is NOT auto-enabled at the workflow level.
		// Each agent determines its own mode based on its actual LLM provider (which may differ from req.Provider
		// due to tiered config). The agent.go layer auto-enables code execution for claude-code/gemini-cli providers.
		// See: agent.go line ~1753 (ProviderClaudeCode auto-enable) and controller_agent_factory.go (provider-based resolution).

		// Use tool search mode from request if preset didn't provide any
		if !useToolSearchMode && req.UseToolSearchMode {
			useToolSearchMode = req.UseToolSearchMode
			log.Printf("[TOOL_SEARCH] Tool search mode enabled from request")
		}

		// Create workflow orchestrator for this request
		// Note: req.MaxTurns is already defaulted to 100 earlier in the handler
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
			useToolSearchMode,    // NEW: tool search mode
			preDiscoveredTools,   // NEW: pre-discovered tools
			allTools,             // customTools
			allExecutors,         // customToolExecutors
			req.LLMConfig,        // llmConfig
			req.MaxTurns,         // maxTurns (defaults to 100 if not provided)
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

		// Propagate CDP port for browser mode detection in execution agents
		if cdpPort := getCdpPort(req); cdpPort > 0 {
			workflowOrchestrator.SetCdpPort(cdpPort)
			log.Printf("[WORKFLOW] Set CDP port on orchestrator: %d", cdpPort)
		}

		// Wire up live tool call query for workshop query_step_tools
		workflowOrchestrator.SetToolCallQueryFunc(formatToolCallSummaries(api))

		// Create a cancellable context for workflow execution using background context
		// This prevents the workflow from being canceled when the HTTP request ends
		workflowCtx, workflowCancel := context.WithCancel(context.Background())

		// Inject user ID into the workflow context for per-user folder isolation
		// This allows workspace tools to route per-user folders correctly
		workflowCtx = context.WithValue(workflowCtx, common.UserIDKey, currentUserID)

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

			// Check database for workflow approval status if preset_id is provided
			workflowStatus := database.WorkflowStatusPreVerification // Default status
			var selectedOptions *database.WorkflowSelectedOptions
			var stepID string
			if req.PresetQueryID != "" {
				// Check workflow approval status from database
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				workflow, err := api.chatDB.GetWorkflowByPresetQueryID(ctx, req.PresetQueryID)
				if err == nil {
					workflowStatus = workflow.WorkflowStatus
					selectedOptions = workflow.SelectedOptions
					log.Printf("[WORKFLOW CHECK] Database check: workflowStatus=%s", workflowStatus)
					if selectedOptions != nil {
						log.Printf("[WORKFLOW CHECK] Found selected options: %+v", selectedOptions)
						log.Printf("[WORKFLOW CHECK] Selected options details - PhaseID: %s, Selections count: %d", selectedOptions.PhaseID, len(selectedOptions.Selections))
						for i, selection := range selectedOptions.Selections {
							log.Printf("[WORKFLOW CHECK] Selection[%d] - Group: %s, OptionID: %s, OptionLabel: %s", i, selection.Group, selection.OptionID, selection.OptionLabel)
						}
					} else {
						log.Printf("[WORKFLOW CHECK] No selected options found")
					}
				} else {
					log.Printf("[WORKFLOW CHECK] Could not check database: %v", err)
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
			if workflowStatus == "workflow-builder" {
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

			// Get the actual objective from preset (not from the query string)
			workflowObjective := req.Query // Default to query if preset not available
			workflowWorkspacePath := ""
			if req.PresetQueryID != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
				if err == nil && preset != nil {
					// Use preset's Query field as the objective (the actual workflow objective)
					// Fall back to req.Query if preset query is empty (e.g. workflow builder sets empty preset query)
					if preset.Query != "" {
						workflowObjective = preset.Query
					}
					log.Printf("[WORKFLOW EXECUTION] Using objective: %s (preset.Query=%q, req.Query=%q)", workflowObjective, preset.Query, req.Query)

					// Extract workspace path from preset's selected folder
					if preset.SelectedFolder.Valid && preset.SelectedFolder.String != "" {
						workflowWorkspacePath = preset.SelectedFolder.String
						log.Printf("[WORKFLOW EXECUTION] Using preset workspace path: %s", workflowWorkspacePath)
					}
				} else {
					log.Printf("[WORKFLOW WARNING] Could not get preset for objective: %v", err)
				}
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
				TriggeredBy:   "manual",
				StartedAt:     time.Now(),
			}
			if req.ExecutionOptions != nil && req.ExecutionOptions.SelectedRunFolder != "" {
				activeExec.RunFolder = req.ExecutionOptions.SelectedRunFolder
			}
			if req.TriggeredBy != "" {
				activeExec.TriggeredBy = req.TriggeredBy
			}
			api.activeWorkflowExecutionsMux.Lock()
			api.activeWorkflowExecutions[queryID] = activeExec
			api.activeWorkflowExecutionsMux.Unlock()

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
				log.Printf("[EXECUTION_OPTIONS_DEBUG] [Backend] Execution options received: %+v", req.ExecutionOptions)
				log.Printf("[WORKFLOW EXECUTION] Frontend execution options provided: run_mode=%s, strategy=%s, run_folder=%s, resume_from_step=%d, enabled_group_ids=%v, skip_learning_temp_llm1=%v, skip_learning_temp_llm2=%v, save_validation_responses=%v",
					req.ExecutionOptions.RunMode, req.ExecutionOptions.ExecutionStrategy, req.ExecutionOptions.SelectedRunFolder, req.ExecutionOptions.ResumeFromStep, req.ExecutionOptions.EnabledGroupIDs, req.ExecutionOptions.SkipLearningWhenTempLLM1, req.ExecutionOptions.SkipLearningWhenTempLLM2, req.ExecutionOptions.SaveValidationResponses)

				// Convert to controller ExecutionOptions and pass to workflow orchestrator
				controllerOpts := &todo_creation_human.ExecutionOptions{
					RunMode:                        req.ExecutionOptions.RunMode,
					SelectedRunFolder:              req.ExecutionOptions.SelectedRunFolder,
					ExecutionStrategy:              req.ExecutionOptions.ExecutionStrategy,
					ResumeFromStep:                 req.ExecutionOptions.ResumeFromStep,
					FastExecuteEndStep:             req.ExecutionOptions.FastExecuteEndStep,
					PlanChangeAction:               req.ExecutionOptions.PlanChangeAction,
					AllStepsCompletedAction:        req.ExecutionOptions.AllStepsCompletedAction,
					FallbackToOriginalLLMOnFailure: req.ExecutionOptions.FallbackToOriginalLLMOnFailure,
					SkipLearningWhenTempLLM1:       req.ExecutionOptions.SkipLearningWhenTempLLM1,
					SkipLearningWhenTempLLM2:       req.ExecutionOptions.SkipLearningWhenTempLLM2,
					EnabledGroupIDs:                req.ExecutionOptions.EnabledGroupIDs,
					SkipExecutionCleanup:           req.ExecutionOptions.SkipExecutionCleanup,
				}

				// Convert TempOverrideLLM if present
				if req.ExecutionOptions.TempOverrideLLM != nil {
					controllerOpts.TempOverrideLLM = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempOverrideLLM.Provider,
						ModelID:  req.ExecutionOptions.TempOverrideLLM.ModelID,
					}
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 1 included: %s/%s", controllerOpts.TempOverrideLLM.Provider, controllerOpts.TempOverrideLLM.ModelID)
				} else {
					// Explicitly set to nil to ensure backend clears any existing override
					controllerOpts.TempOverrideLLM = nil
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 1 not provided (disabled or not set) - will clear existing override")
				}

				// Convert TempOverrideLLM2 if present
				if req.ExecutionOptions.TempOverrideLLM2 != nil {
					controllerOpts.TempOverrideLLM2 = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempOverrideLLM2.Provider,
						ModelID:  req.ExecutionOptions.TempOverrideLLM2.ModelID,
					}
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 2 included: %s/%s", controllerOpts.TempOverrideLLM2.Provider, controllerOpts.TempOverrideLLM2.ModelID)
				} else {
					// Explicitly set to nil to ensure backend clears any existing override
					controllerOpts.TempOverrideLLM2 = nil
					log.Printf("[WORKFLOW EXECUTION] Temp override LLM 2 not provided (disabled or not set) - will clear existing override")
				}

				// Convert TempLearningLLM if present
				if req.ExecutionOptions.TempLearningLLM != nil {
					controllerOpts.TempLearningLLM = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempLearningLLM.Provider,
						ModelID:  req.ExecutionOptions.TempLearningLLM.ModelID,
					}
					log.Printf("[WORKFLOW EXECUTION] Temp learning LLM included: %s/%s", controllerOpts.TempLearningLLM.Provider, controllerOpts.TempLearningLLM.ModelID)
				} else {
					// Explicitly set to nil to ensure backend clears any existing override
					controllerOpts.TempLearningLLM = nil
					log.Printf("[WORKFLOW EXECUTION] Temp learning LLM not provided - will clear existing override")
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
							if presetLLMConfig.ExecutionLLM != nil {
								models.ExecutionLLM = &RunMetadataLLM{Provider: presetLLMConfig.ExecutionLLM.Provider, ModelID: presetLLMConfig.ExecutionLLM.ModelID}
							}
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
						if req.ExecutionOptions.TempOverrideLLM != nil {
							models.TempOverride = &RunMetadataLLM{Provider: req.ExecutionOptions.TempOverrideLLM.Provider, ModelID: req.ExecutionOptions.TempOverrideLLM.ModelID}
						}
						if req.ExecutionOptions.TempOverrideLLM2 != nil {
							models.TempOverride2 = &RunMetadataLLM{Provider: req.ExecutionOptions.TempOverrideLLM2.Provider, ModelID: req.ExecutionOptions.TempOverrideLLM2.ModelID}
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

				// --- BEGIN: Update chat session status to error ---
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				chatSession, err := api.chatDB.GetChatSession(ctx, sessionID)
				cancel()
				if err == nil && chatSession != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Title:     chatSession.Title,     // Preserve existing title
						AgentMode: chatSession.AgentMode, // Preserve existing agent_mode
						Status:    "error",
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, err := api.chatDB.UpdateChatSession(ctx, sessionID, updateReq)
					cancel()
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (workflow): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status (workflow)", sessionID)
					}
				}
				// --- END: Update chat session status to error ---

				// Update active session status to error
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to error", sessionID)
				api.updateSessionStatus(sessionID, "error")
				// Clean up HTTP session → MCP session tracker on error completion
				mcpagent.CloseHTTPSession(sessionID)
			} else {
				log.Printf("[WORKFLOW DEBUG] Workflow execution completed for query %s", queryID)
				// Workflow completion events are now handled by the workflow orchestrator itself

				// --- BEGIN: Update chat session status to completed ---
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				chatSession, err := api.chatDB.GetChatSession(ctx, sessionID)
				cancel()
				if err == nil && chatSession != nil {
					// Update session status to completed with completion timestamp
					completedAt := time.Now()
					updateReq := &database.UpdateChatSessionRequest{
						Status:      "completed",
						CompletedAt: &completedAt,
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, err := api.chatDB.UpdateChatSession(ctx, sessionID, updateReq)
					cancel()
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to completed (workflow): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to completed status (workflow)", sessionID)
					}
				}
				// --- END: Update chat session status to completed ---

				// Update active session status to completed
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to completed", sessionID)
				api.updateSessionStatus(sessionID, "completed")
				// Clean up HTTP session → MCP session tracker on successful completion
				mcpagent.CloseHTTPSession(sessionID)
			}
		}()
		return
	}

	// Load preset LLM config for chat/simple mode (for feature toggle fallbacks)
	var presetLLMConfig *database.PresetLLMConfig
	if req.PresetQueryID != "" {
		ctx := context.Background()
		preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
		if err == nil && len(preset.LLMConfig) > 0 {
			if err := json.Unmarshal(preset.LLMConfig, &presetLLMConfig); err != nil {
				log.Printf("[PRESET LLM] Failed to parse preset LLM config for chat mode: %v", err)
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
	if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
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
				log.Printf("[SYNTHETIC_TURN] Cancelling synthetic turn for session %s — user message takes priority", sessionID)
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

	// Process the query in the background
	go func() {
		// Clear session busy when the agent turn completes
		if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
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

				// Update chat session status to error
				if chatSession != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Title:     chatSession.Title,     // Preserve existing title
						AgentMode: chatSession.AgentMode, // Preserve existing agent_mode
						Status:    "error",
					}
					_, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, updateReq)
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error: %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status", sessionID)
					}
				}

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

		// Validate provider (use finalProvider which reflects LLMConfig.Primary.Provider)
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
		var useToolSearchMode bool
		var presetSetCodeExecutionMode bool // Track if preset explicitly set the value

		if req.PresetQueryID != "" {
			ctx := context.Background()
			preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
			if err == nil {
				if preset.SelectedTools != "" {
					if err := json.Unmarshal([]byte(preset.SelectedTools), &selectedTools); err != nil {
						log.Printf("[TOOLS] Failed to parse selected tools from preset: %v", err)
					} else {
						if len(selectedTools) > 0 {
							log.Printf("[TOOLS] Loaded %d specific tools from preset", len(selectedTools))
						} else {
							log.Printf("[TOOLS] Preset has empty tool selection - will use ALL tools from selected servers")
						}
					}
				}
				// Load code execution mode from preset
				useCodeExecutionMode = preset.UseCodeExecutionMode
				presetSetCodeExecutionMode = true
				if useCodeExecutionMode {
					log.Printf("[CODE_EXECUTION] Code execution mode enabled from preset")
				} else {
					log.Printf("[CODE_EXECUTION] Code execution mode disabled from preset")
				}
				// Load tool search mode from preset
				useToolSearchMode = preset.UseToolSearchMode
				if useToolSearchMode {
					log.Printf("[TOOL_SEARCH] Tool search mode enabled from preset")
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

		// CRITICAL: Always respect request value when explicitly set (frontend always sends explicit value)
		// The frontend explicitly sends use_code_execution_mode (true or false), so we should use it
		// This ensures that when user selects "simple" mode without preset, the request value is respected
		useCodeExecutionMode = req.UseCodeExecutionMode
		if useCodeExecutionMode {
			log.Printf("[CODE_EXECUTION] Code execution mode enabled from request")
		} else {
			if presetSetCodeExecutionMode {
				log.Printf("[CODE_EXECUTION] Code execution mode disabled by request (overriding preset value)")
			} else {
				log.Printf("[CODE_EXECUTION] Code execution mode disabled (default)")
			}
		}

		// Auto-enable code execution mode for claude-code provider.
		// Claude Code accesses MCP tools via the HTTP bridge (mcpbridge stdio binary),
		// which requires code execution mode to expose per-tool API endpoints.
		if req.Provider == "claude-code" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[CLAUDE CODE] Auto-enabled code execution mode for MCP tool access via bridge")
		}

		// Auto-enable code execution mode for gemini-cli provider.
		// Gemini CLI accesses MCP tools via the pre-configured bridge,
		// which requires code execution mode to expose per-tool API endpoints.
		if req.Provider == "gemini-cli" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[GEMINI CLI] Auto-enabled code execution mode for MCP tool access via bridge")
		}

		// Auto-enable code execution mode for codex-cli provider.
		// Codex CLI accesses MCP tools via the pre-configured bridge,
		// which requires code execution mode to expose per-tool API endpoints.
		if req.Provider == "codex-cli" && !useCodeExecutionMode {
			useCodeExecutionMode = true
			log.Printf("[CODEX CLI] Auto-enabled code execution mode for MCP tool access via bridge")
		}

		// CRITICAL: Always respect request value for tool search mode when explicitly set
		// The frontend explicitly sends use_tool_search_mode (true or false), so we should use it
		useToolSearchMode = req.UseToolSearchMode
		if useToolSearchMode {
			log.Printf("[TOOL_SEARCH] Tool search mode enabled from request")
		}

		// In plan delegation mode, the orchestrator should NEVER use code execution mode
		// UNLESS the provider is claude-code (which requires it for MCP bridge tool access).
		// The orchestrator only researches, plans, and delegates — it should not write/execute code itself.
		// Sub-agents get their mode from the explicit tool_mode parameter on the delegate call.
		// However, tool search mode is auto-enabled when many MCP servers are selected (>3)
		// so the orchestrator can efficiently discover tools for research.
		if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
			if useCodeExecutionMode && req.Provider != "claude-code" && req.Provider != "gemini-cli" && req.Provider != "codex-cli" {
				log.Printf("[CODE_EXECUTION] Disabling code execution mode for orchestrator in plan delegation mode")
				useCodeExecutionMode = false
			}
			// Count total tools across selected servers to decide on tool search mode
			api.toolStatusMux.RLock()
			totalToolCount := 0
			for _, s := range selectedServers {
				if s != "all" && s != mcpclient.NoServers {
					if status, ok := api.toolStatus[s]; ok {
						totalToolCount += status.ToolsEnabled
					}
				}
			}
			api.toolStatusMux.RUnlock()
			if totalToolCount >= 10 && !useToolSearchMode {
				log.Printf("[TOOL_SEARCH] Auto-enabling tool search mode for orchestrator — %d tools across selected servers (>=10)", totalToolCount)
				useToolSearchMode = true
			} else if useToolSearchMode && totalToolCount < 10 {
				log.Printf("[TOOL_SEARCH] Disabling tool search mode for orchestrator — only %d tools across selected servers (<10)", totalToolCount)
				useToolSearchMode = false
			}
		}

		// Workflow phase agents with many custom tools always use tool search mode —
		// they get 21+ workshop tools registered after agent creation, which aren't counted in
		// the MCP tool count above. Force tool search mode so these tools are discoverable.
		if isWorkflowPhase && workflowPhaseID == "workflow-builder" {
			if !useToolSearchMode {
				log.Printf("[TOOL_SEARCH] Forcing tool search mode for %s phase (21+ workshop tools)", workflowPhaseID)
				useToolSearchMode = true
			}
		}

		// In plan delegation mode, orchestrator uses Main tier model (falls back to High if Main not set)
		if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
			tierConfig := resolveDelegationTierConfig(req.DelegationTierConfig)
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
		log.Printf("[AGENT CONFIG DEBUG] Creating agent with ServerName: %s, UseCodeExecutionMode: %v, UseToolSearchMode: %v", serverList, useCodeExecutionMode, useToolSearchMode)
		agentConfig := agent.LLMAgentConfig{
			Name:               sessionID,
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
			// Tool search mode: When enabled, LLM discovers tools on-demand via search_tools
			UseToolSearchMode: useToolSearchMode,
			// Pre-discovered tools: all virtual/custom tools stay visible in tool search mode
			// Only MCP server tools require discovery via search_tools
			PreDiscoveredTools: func() []string {
				if !useToolSearchMode {
					return nil
				}
				// Collect all virtual tool names so they're never hidden behind search
				preDiscovered := collectVirtualToolNames(
					virtualtools.CreateWorkspaceAdvancedTools(),
					virtualtools.CreateHumanTools(),
				)
				if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
					// In plan mode, only async tools are registered
					preDiscovered = append(preDiscovered, "delegate", "query_agent", "terminate_agent", "list_agents")
				} else if req.DelegationMode == "spawn" {
					// Spawn mode remains lightweight, but planner tool stays available when needed.
					preDiscovered = append(preDiscovered, "delegate", "create_delegation_plan")
				}
				if req.EnableBrowserAccess != nil && *req.EnableBrowserAccess {
					preDiscovered = append(preDiscovered, collectVirtualToolNames(virtualtools.CreateWorkspaceBrowserTools())...)
				}
				return preDiscovered
			}(),
			// Convert API keys from request to LLM format (respecting locked providers)
			APIKeys: func() *llm.ProviderAPIKeys {
				// 1. Start with keys from request (if available)
				llmKeys := &llm.ProviderAPIKeys{}
				if req.LLMConfig != nil && req.LLMConfig.APIKeys != nil {
					llmKeys.OpenRouter = req.LLMConfig.APIKeys.OpenRouter
					llmKeys.OpenAI = req.LLMConfig.APIKeys.OpenAI
					llmKeys.Anthropic = req.LLMConfig.APIKeys.Anthropic
					llmKeys.Vertex = req.LLMConfig.APIKeys.Vertex
					llmKeys.GeminiCLI = req.LLMConfig.APIKeys.GeminiCLI
					llmKeys.MiniMax = req.LLMConfig.APIKeys.MiniMax
					llmKeys.MiniMaxCodingPlan = req.LLMConfig.APIKeys.MiniMaxCodingPlan

					if req.LLMConfig.APIKeys.Bedrock != nil {
						llmKeys.Bedrock = &llm.BedrockConfig{
							Region: req.LLMConfig.APIKeys.Bedrock.Region,
						}
					}
					if req.LLMConfig.APIKeys.Azure != nil {
						llmKeys.Azure = &llm.AzureAPIConfig{
							Endpoint:   req.LLMConfig.APIKeys.Azure.Endpoint,
							APIKey:     req.LLMConfig.APIKeys.Azure.APIKey,
							APIVersion: req.LLMConfig.APIKeys.Azure.APIVersion,
							Region:     req.LLMConfig.APIKeys.Azure.Region,
						}
					}
				}

				// 2. Get keys from environment
				envKeys := buildProviderAPIKeysFromEnv()
				globalLocked := isGlobalLLMConfigLocked()

				// 3. Override if provider is locked or global lock is on
				if globalLocked || isProviderLocked("openrouter") {
					llmKeys.OpenRouter = envKeys.OpenRouter
				}
				if globalLocked || isProviderLocked("openai") {
					llmKeys.OpenAI = envKeys.OpenAI
				}
				if globalLocked || isProviderLocked("anthropic") {
					llmKeys.Anthropic = envKeys.Anthropic
				}
				if globalLocked || isProviderLocked("vertex") {
					llmKeys.Vertex = envKeys.Vertex
				}
				if globalLocked || isProviderLocked("bedrock") {
					llmKeys.Bedrock = envKeys.Bedrock
				}
				if globalLocked || isProviderLocked("azure") {
					llmKeys.Azure = envKeys.Azure
				}
				if globalLocked || isProviderLocked("gemini-cli") {
					llmKeys.GeminiCLI = envKeys.GeminiCLI
				}
				if globalLocked || isProviderLocked("minimax") {
					llmKeys.MiniMax = envKeys.MiniMax
				}
				if globalLocked || isProviderLocked("minimax-coding-plan") {
					llmKeys.MiniMaxCodingPlan = envKeys.MiniMaxCodingPlan
				}

				return llmKeys
			}(),
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
			// [BROWSER_UPLOAD] Dynamically override Playwright MCP server config per user.
			// Playwright restricts file access to its working_dir (cwd). By default the static
			// config points to ../workspace-docs/Downloads, but per-user files live at
			// _users/{userId}/Downloads, _users/{userId}/Chats, etc. This override sets:
			//   working_dir  → _users/{userId}/       (allows access to all user subfolders)
			//   --output-dir → _users/{userId}/Downloads (browser downloads go to user's folder)
			// Without this, Playwright rejects uploads with "outside allowed roots" errors.
			RuntimeOverrides: func() mcpclient.RuntimeOverrides {
				userFolder := currentUserID
				if userFolder == "" {
					userFolder = "default"
				}
				userWorkspacePath := filepath.Join("..", "workspace-docs", "_users", userFolder)
				userDownloadsPath := filepath.Join(userWorkspacePath, "Downloads")
				log.Printf("[BROWSER_UPLOAD] Playwright runtime override: working_dir=%s, output-dir=%s", userWorkspacePath, userDownloadsPath)
				return mcpclient.RuntimeOverrides{
					"playwright": {
						WorkingDir:  userWorkspacePath,
						ArgsReplace: map[string]string{"--output-dir": userDownloadsPath},
					},
				}
			}(),
		}

		// Set agent mode based on request
		switch req.AgentMode {
		case "simple":
			agentConfig.AgentMode = mcpagent.SimpleAgent
		case "orchestrator":
			// For orchestrator mode, we'll handle it differently
			agentConfig.AgentMode = mcpagent.SimpleAgent // Use Simple as base for orchestrator
		case "workflow":
			// For workflow mode, we'll handle it differently
			agentConfig.AgentMode = mcpagent.SimpleAgent // Use Simple as base for workflow
		default:
			agentConfig.AgentMode = mcpagent.SimpleAgent // Default to Simple mode
		}
		log.Printf("[AGENT DEBUG] Creating agent with mode: %s, servers: %s", agentConfig.AgentMode, serverList)
		log.Printf("[SMART ROUTING DEBUG] Smart routing enabled - MaxTools: %d, MaxServers: %d (using defaults for temperature/tokens)",
			agentConfig.SmartRoutingMaxTools, agentConfig.SmartRoutingMaxServers)
		log.Printf("[LATENCY_DEBUG] T+%dms | Agent config built, creating agent wrapper | provider=%s model=%s", time.Since(startTime).Milliseconds(), finalProvider, finalModelID)
		// Create LLM agent wrapper with trace using streamCtx
		llmAgent, err := agent.NewLLMAgentWrapperWithTrace(streamCtx, agentConfig, tracer, traceID, api.logger)
		if err != nil {
			log.Printf("[AGENT DEBUG] Failed to create LLM agent wrapper: %v", err)
			sendError(fmt.Sprintf("Failed to create agent: %v", err), true)
			return
		}
		log.Printf("[LATENCY_DEBUG] T+%dms | Agent wrapper created", time.Since(startTime).Milliseconds())

		// Add workspace tools to simple agents (chat mode)
		// This matches how workspace tools are registered in workflow/orchestrator agents
		// This ensures custom tools are available and code generation is triggered
		// Only register workspace tools if workspace access is enabled
		// Note: Frontend always sends enable_workspace_access for chat mode (true/false)
		// Chat mode is detected by: "simple", "" (empty/default), or "chat" agent mode
		// Workflow/orchestrator modes handle workspace tools differently, so exclude them
		isChatMode := req.AgentMode == "simple" || req.AgentMode == "" || req.AgentMode == "chat" || isOrganizationChat

		// Check if skill-creator is in selected skills (mode-agnostic)
		hasSkillCreator := false
		for _, s := range req.SelectedSkills {
			if s == "skill-creator" {
				hasSkillCreator = true
				break
			}
		}

		// Check if subagent-creator is in selected skills
		hasSubAgentCreator := false
		for _, s := range req.SelectedSkills {
			if s == "subagent-creator" || s == "custom/subagent-creator" {
				hasSubAgentCreator = true
				break
			}
		}

		// When skill-creator is selected, ensure it's installed
		if hasSkillCreator {
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
		if isChatMode && llmAgent.GetUnderlyingAgent() != nil {
			// Check if workspace access is enabled
			// Default to true for backward compatibility with legacy requests
			// nil = inherit default (true), non-nil = explicit override
			enableWorkspaceAccess := true // Default to enabled for backward compatibility
			if req.EnableWorkspaceAccess != nil {
				enableWorkspaceAccess = *req.EnableWorkspaceAccess
			}
			// Automatically enable workspace access when skills are selected
			// Skills need workspace access to read files and context
			if len(req.SelectedSkills) > 0 {
				enableWorkspaceAccess = true
				log.Printf("[SKILLS] Automatically enabling workspace access (skills selected: %v)", req.SelectedSkills)
			}

			// Auto-enable workspace access for plan delegation mode
			// Plan mode requires workspace tools for shell commands with proper FolderGuard
			if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
				enableWorkspaceAccess = true
			}

			if isOrganizationChat {
				enableWorkspaceAccess = true
			}

			// Handle browser access: when enabled, auto-enable workspace and add agent-browser skill
			enableBrowserAccess := false
			if req.EnableBrowserAccess != nil && *req.EnableBrowserAccess {
				enableBrowserAccess = true
				enableWorkspaceAccess = true // Browser tool lives in workspace category
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

			if enableWorkspaceAccess {
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

				// Chat mode: advanced workspace tools (shell, image, web fetch, PDF, diff_patch)
				// Basic tools (list/read/write/search) and git tools are not needed — shell is sufficient
				// These tools will be RESTRICTED to Chats/ folder via wrapExecutorsWithChatModeFolderGuard
				workspaceTools := virtualtools.CreateWorkspaceAdvancedTools()
				var workspaceExecutors map[string]func(ctx context.Context, args map[string]interface{}) (string, error)
				workspaceExecutors, workspaceEnv = virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(currentUserID, sessionID)
				log.Printf("[USER_ID_DEBUGGING] Main agent workspace executors: created with explicit userID=%q sessionID=%q", currentUserID, sessionID)
				// Inject LLM config fallback for read_image HTTP calls (e.g., from claude CLI subprocess)
				if underlying := llmAgent.GetUnderlyingAgent(); underlying != nil {
					virtualtools.SetReadImageFallbackLLMConfig(workspaceExecutors, underlying.GetLLMModelConfig())
				}
				_, _, toolCategories := createCustomTools(false) // Get toolCategories map (advanced only)

				// Extract @context file paths for additional write access
				fileContextFolders := extractFileContextWriteFolders(req.Query)
				if len(fileContextFolders) > 0 {
					log.Printf("[FILE CONTEXT] Extracted write paths from @context: %v", fileContextFolders)
				}

				// Workflow phase: grant write access only to specific subfolders within the workflow folder.
				// planning/ is intentionally excluded (read-only for the workflow builder).
				// Full read access to the workflow folder is unrestricted by the guard.
				if isWorkflowPhase && workflowPhaseFolder != "" {
					workflowWriteSubfolders := []string{"knowledgebase/", "execution/", "learnings/", "scripts/", "runs/"}
					for _, sub := range workflowWriteSubfolders {
						fileContextFolders = append(fileContextFolders, workflowPhaseFolder+"/"+sub)
					}
					log.Printf("[WORKFLOW_PHASE FOLDER GUARD] Write access restricted to subfolders of %s: %v", workflowPhaseFolder, workflowWriteSubfolders)
				}

				// Apply folder guard to restrict writes based on mode
				// Multi-agent (plan) mode: primary write folder is Plans/, Chats/ also writable
				// Chat mode: writes go to Chats/
				if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
					additionalFolders := []string{"Chats/"}
					if hasSkillCreator {
						additionalFolders = append(additionalFolders, "skills/custom/")
					}
					if hasSubAgentCreator {
						additionalFolders = append(additionalFolders, "subagents/custom/")
					}
					additionalFolders = append(additionalFolders, fileContextFolders...)
					workspaceExecutors = wrapExecutorsWithPlanFolderGuard(workspaceExecutors, "Plans", additionalFolders...)
					workspace.SetSessionWorkingDir(sessionID, "Plans/")
					workspace.SetSessionFolderGuard(sessionID,
						append([]string{"Plans/", "skills/", "subagents/", "Downloads/"}, additionalFolders...),
						[]string{"Plans/"},
					)
					log.Printf("[MULTI-AGENT FOLDER GUARD] Applied Plans/ folder restriction (additional: %v)", additionalFolders)
				} else {
					extraFolders := []string{}
					if hasSkillCreator {
						extraFolders = append(extraFolders, "skills/custom/")
					}
					if hasSubAgentCreator {
						extraFolders = append(extraFolders, "subagents/custom/")
					}
					extraFolders = append(extraFolders, fileContextFolders...)
					workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors, extraFolders...)
					workspace.SetSessionWorkingDir(sessionID, "Chats/")
					workspace.SetSessionFolderGuard(sessionID,
						append([]string{"Chats/", "Downloads/", "Plans/", "skills/", "subagents/", "Workflow/"}, extraFolders...),
						append([]string{"Chats/", "Downloads/"}, extraFolders...),
					)
					log.Printf("[CHAT MODE FOLDER GUARD] Applied Chats/ + %v folder restriction", extraFolders)
				}

				// Apply skill folder guard if skills are selected (read-only access to selected skills only)
				if len(req.SelectedSkills) > 0 {
					log.Printf("[SKILL FOLDER GUARD] Applied skill folder restriction - only selected skills accessible: %v", req.SelectedSkills)
				}

				log.Printf("[WORKSPACE TOOLS] Registering %d workspace advanced tools for chat mode (enable_workspace_access: %v)", len(workspaceTools), enableWorkspaceAccess)

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
						if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
							enhancedDescription = enhanceToolDescriptionForPlanMode(toolName, tool.Function.Description)
						} else {
							enhancedDescription = enhanceToolDescriptionForChatMode(toolName, tool.Function.Description)
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
				log.Printf("[WORKSPACE TOOLS] Successfully registered %d workspace advanced tools for chat mode", len(workspaceTools))

				// Register browser tool if browser access is enabled
				if enableBrowserAccess {
					browserTools := virtualtools.CreateWorkspaceBrowserTools()
					browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutorsWithSession(sessionID, getCdpPort(req))
					browserCategory := virtualtools.GetWorkspaceBrowserToolCategory()

					// Apply same folder guard as workspace tools (reuse fileContextFolders from above)
					if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
						additionalFolders := []string{"Chats/"}
						if hasSkillCreator {
							additionalFolders = append(additionalFolders, "skills/custom/")
						}
						additionalFolders = append(additionalFolders, fileContextFolders...)
						browserExecutors = wrapExecutorsWithPlanFolderGuard(browserExecutors, "Plans", additionalFolders...)
					} else {
						browserExtraFolders := []string{}
						if hasSkillCreator {
							browserExtraFolders = append(browserExtraFolders, "skills/custom/")
						}
						browserExtraFolders = append(browserExtraFolders, fileContextFolders...)
						browserExecutors = wrapExecutorsWithChatModeFolderGuard(browserExecutors, browserExtraFolders...)
					}
					log.Printf("[BROWSER TOOLS] Applied folder guard to browser tools (delegation_mode: %s)", req.DelegationMode)

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

			} else {
				log.Printf("[WORKSPACE TOOLS] Skipping workspace tools registration (enable_workspace_access: false)")
			}

			// Register image generation tools if enabled
			// NOTE: This is OUTSIDE the enableWorkspaceAccess block intentionally —
			// image gen should work regardless of workspace access setting.
			enableImgGen := req.EnableImageGeneration != nil && *req.EnableImageGeneration
			log.Printf("[IMAGE GEN] enable_image_generation check: ptr_set=%v enabled=%v image_gen_config_nil=%v", req.EnableImageGeneration != nil, enableImgGen, req.ImageGenConfig == nil)
			if req.ImageGenConfig != nil {
				log.Printf("[IMAGE GEN] received image_gen_config: provider=%q model_id=%q api_key_set=%v", req.ImageGenConfig.Provider, req.ImageGenConfig.ModelID, req.ImageGenConfig.APIKey != "")
			}
			if enableImgGen {
				imgAgent := llmAgent.GetUnderlyingAgent()
				if imgAgent == nil {
					log.Printf("[IMAGE GEN] Warning: underlying agent is nil, cannot register image gen tools")
				} else {
					imgCfg := virtualtools.ImageGenExecutorConfig{
						Provider:        "vertex",
						ModelID:         "gemini-2.5-flash-image",
						WorkspaceAPIURL: getWorkspaceAPIURL(),
						UserID:          currentUserID,
					}
					if req.ImageGenConfig != nil {
						if req.ImageGenConfig.Provider != "" {
							imgCfg.Provider = req.ImageGenConfig.Provider
						}
						if req.ImageGenConfig.ModelID != "" {
							imgCfg.ModelID = req.ImageGenConfig.ModelID
						}
						imgCfg.APIKey = req.ImageGenConfig.APIKey
					}
					for _, toolDef := range []struct {
						tool     func() llmtypes.Tool
						executor func(virtualtools.ImageGenExecutorConfig) func(context.Context, map[string]any) (string, error)
						category func() string
					}{
						{virtualtools.GetImageGenToolDefinition, virtualtools.CreateImageGenExecutor, virtualtools.GetImageGenToolCategory},
						{virtualtools.GetImageEditToolDefinition, virtualtools.CreateImageEditExecutor, virtualtools.GetImageEditToolCategory},
					} {
						t := toolDef.tool()
						exec := toolDef.executor(imgCfg)
						var params map[string]interface{}
						if t.Function.Parameters != nil {
							paramsBytes, err := json.Marshal(t.Function.Parameters)
							if err == nil {
								json.Unmarshal(paramsBytes, &params)
							}
						}
						if params != nil {
							if err := imgAgent.RegisterCustomTool(
								t.Function.Name,
								t.Function.Description,
								params,
								exec,
								toolDef.category(),
							); err != nil {
								log.Printf("[IMAGE GEN] Warning: Failed to register %s: %v", t.Function.Name, err)
							} else {
								log.Printf("[IMAGE GEN] Registered %s (provider=%s model=%s)", t.Function.Name, imgCfg.Provider, imgCfg.ModelID)
							}
						}
					}
				}
			}

			// Register delegation tool if delegation mode is enabled
			// Note: This is outside the workspace access block because delegation should work regardless of workspace access
			delegationMode := req.DelegationMode // "spawn", "plan", or ""

			if delegationMode == "spawn" || delegationMode == "plan" {
				// Build delegation tier config early so we can pass it to tool creation (for dynamic enum)
				tierConfig := resolveDelegationTierConfig(req.DelegationTierConfig)
				delegationTools := virtualtools.CreateDelegationTools(tierConfig, true)
				delegationExecutors := virtualtools.CreateDelegationToolExecutors()
				delegationCategory := virtualtools.GetDelegationToolCategory()

				// Get underlying agent for tool registration
				delegationAgent := llmAgent.GetUnderlyingAgent()
				if delegationAgent == nil {
					log.Printf("[DELEGATION TOOLS ERROR] Cannot register delegation tools - underlying agent is nil")
				} else {
					// Create the delegation execution function that will spawn sub-agents
					// This function is injected into the context for the delegate tool to use
					executeDelegatedTask := func(subCtx context.Context, instruction string) (string, error) {
						return api.executeDelegatedTask(subCtx, req, sessionID, instruction)
					}

					// Create workspace client for plan file I/O.
					planWriteFolder := "Plans"
					planWorkspaceClient := workspace.NewClient(
						getWorkspaceAPIURL(),
						workspace.WithFolderGuard(&workspace.FolderGuardConfig{
							Enabled:      true,
							WritePaths:   []string{planWriteFolder},
							BlockedPaths: []string{"_users"},
						}),
						workspace.WithUserID(currentUserID),
					)

					// Build capabilities context for the planner
					caps := buildCapabilitiesContext(req)

					// Get or create session-level plan state (replaces per-message PlanTracker)
					planState := api.getOrCreatePlanSessionState(sessionID)

					// If client passed an existing plan folder, pre-seed the session state so LLM reuses it
					if req.PlanFolder != "" && planState.PlanID == "" {
						parts := strings.SplitN(req.PlanFolder, "/", 2)
						if len(parts) == 2 && parts[0] == virtualtools.PlanFileFolderPath {
							planState.PlanID = parts[1]
							planState.PlanFolder = req.PlanFolder
							log.Printf("[PLAN STATE] Pre-seeded plan from request: %s", req.PlanFolder)
						}
					}

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
								log.Printf("[DELEGATION TOOLS] Warning: Failed to convert parameters for tool %s", toolName)
								continue
							}

							// Capture executor for closure
							exec := executor

							// Wrap the executor to inject delegation function, workspace client, tier config, capabilities, and plan tracker
							wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
								ctx = context.WithValue(ctx, virtualtools.ExecuteDelegatedTaskKey, virtualtools.ExecuteDelegatedTaskFunc(executeDelegatedTask))
								ctx = context.WithValue(ctx, virtualtools.WorkspaceClientKey, planWorkspaceClient)
								ctx = context.WithValue(ctx, virtualtools.PlanEventEmitterKey, &planEventEmitter{
									eventStore: api.eventStore,
									sessionID:  sessionID,
									chatDB:     api.chatDB,
								})
								ctx = context.WithValue(ctx, virtualtools.PlanSessionStateKey, planState)
								ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, &sessionEventEmitter{
									eventStore: api.eventStore,
									sessionID:  sessionID,
									chatDB:     api.chatDB,
								})
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
								log.Printf("[DELEGATION TOOLS ERROR] Failed to register tool %s: %v", toolName, err)
								continue
							}
							log.Printf("[DELEGATION TOOLS] Registered delegation tool: %s (category: %s)", toolName, delegationCategory)
						}
					}
					log.Printf("[DELEGATION TOOLS] Successfully registered %d delegation tools for chat mode", len(delegationTools))
				}
			}
		}

		// Add custom agent instructions based on agent mode
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			// Create custom tools for chat mode (workspace_advanced only: shell, image, web fetch, PDF)
			allTools, allExecutors, toolCategories := createCustomTools(false) // Chat mode: workspace_advanced only

			// In plan delegation mode (multi-agent), also include human tools (human_feedback)
			// createCustomTools(false) only returns workspace_advanced; human tools need to be added explicitly
			if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
				humanCategory := virtualtools.GetHumanToolCategory()
				for _, tool := range virtualtools.CreateHumanTools() {
					allTools = append(allTools, tool)
					if tool.Function != nil {
						toolCategories[tool.Function.Name] = humanCategory
					}
				}
				for name, executor := range virtualtools.CreateHumanToolExecutors() {
					allExecutors[name] = executor
				}
			}

			// Register each custom tool with the agent
			// This will trigger code generation and update the registry
			// Note: Workspace tools are already registered above, skip them in allTools
			registeredCount := 0
			for _, tool := range allTools {
				if tool.Function != nil {
					toolName := tool.Function.Name

					// Skip workspace tools - already registered above
					if toolCategories[toolName] == "workspace_tools" {
						continue
					}

					// Human tools: skip in regular chat mode, allow in plan delegation mode
					if toolCategories[toolName] == virtualtools.GetHumanToolCategory() {
						if req.DelegationMode != "plan" {
							continue
						}
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
										chatDB:     api.chatDB,
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
			memoryTools := virtualtools.CreateMemoryTools()
			memoryExecutors := virtualtools.CreateMemoryToolExecutors()
			memoryCategory := virtualtools.GetDelegationToolCategory()
			var memoryWritePaths []string
			memoryWritePaths = []string{"Plans"}
			memoryWorkspaceClient := workspace.NewClient(
				getWorkspaceAPIURL(),
				workspace.WithFolderGuard(&workspace.FolderGuardConfig{
					Enabled:      true,
					WritePaths:   memoryWritePaths,
					BlockedPaths: []string{"_users"},
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
					// Inject per-session memory folder override
					api.activeSessionsMux.RLock()
					sess, sessExists := api.activeSessions[sessionID]
					api.activeSessionsMux.RUnlock()
					if sessExists && sess.MemoryFolder != "" {
						ctx = context.WithValue(ctx, virtualtools.MemoryFolderKey, sess.MemoryFolder)
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

			if isOrganizationChat {
				if err := api.registerOrganizationChatTools(underlyingAgent, currentUserID, sessionID); err != nil {
					log.Printf("[ORG CHAT] Failed to register organization tools: %v", err)
					sendError(fmt.Sprintf("Failed to register organization tools: %v", err), true)
					return
				}
				log.Printf("[ORG CHAT] Registered organization tools")
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

			// Add base instructions — skip for plan mode (multi-agent) since the
			// main agent is an orchestrator, not a file writer. The Chats/ folder
			// rules don't apply; background sub-agents get their own instructions.
			if req.DelegationMode != "plan" {
				underlyingAgent.AppendSystemPrompt(GetAgentInstructions())
			}

			if isOrganizationChat {
				underlyingAgent.AppendSystemPrompt(getOrganizationChatInstructions())
			}

			// Add skill builder instructions when skill-creator is active
			if hasSkillCreator {
				underlyingAgent.AppendSystemPrompt(GetSkillBuilderInstructions())
				log.Printf("[SKILL BUILDER] Added skill builder instructions to system prompt")
			}

			// Add sub-agent builder instructions when subagent-creator is active
			if hasSubAgentCreator {
				underlyingAgent.AppendSystemPrompt(GetSubAgentBuilderInstructions())
				log.Printf("[SUBAGENT BUILDER] Added sub-agent builder instructions to system prompt")
			}

			// Add skill instructions if skills are selected
			if len(req.SelectedSkills) > 0 {
				skillPrompt := buildSkillPrompt(req.SelectedSkills, getWorkspaceAPIURL())
				if skillPrompt != "" {
					underlyingAgent.AppendSystemPrompt(skillPrompt)
					log.Printf("[SKILLS] Added skill instructions to system prompt (%d skills)", len(req.SelectedSkills))
				}
			}

			// Add browser instructions (upload + mode-specific) using standardized builder
			chatBrowserCfg := buildChatBrowserConfig(req)
			if chatBrowserPrompt := browserinstructions.BuildBrowserInstructions(chatBrowserCfg); chatBrowserPrompt != "" {
				underlyingAgent.AppendSystemPrompt(chatBrowserPrompt)
				log.Printf("[BROWSER] Added browser instructions to system prompt (playwright=%v, camofox=%v, agent-browser=%v, cdp=%v)",
					chatBrowserCfg.HasPlaywright, chatBrowserCfg.HasCamofox, chatBrowserCfg.HasAgentBrowser, chatBrowserCfg.CdpPort > 0)
			}

			// Add inline quick-start instructions for GWS access
			if req.EnableGWSAccess != nil && *req.EnableGWSAccess {
				underlyingAgent.AppendSystemPrompt(getGWSQuickStartInstructions())
				log.Printf("[GWS] Added GWS quick-start instructions to system prompt")
			}

			// Add workflow context if workflow paths are selected (via # in chat)
			if len(req.WorkflowContextPaths) > 0 {
				workflowPrompt := buildWorkflowContextPrompt(req.WorkflowContextPaths, getWorkspaceAPIURL())
				if workflowPrompt != "" {
					underlyingAgent.AppendSystemPrompt(workflowPrompt)
					log.Printf("[WORKFLOW-CTX] Added workflow context to system prompt (%d workflows)", len(req.WorkflowContextPaths))
				}
			}

			// Add delegation instructions — unified autonomous mode for all delegation modes
			if req.DelegationMode == "plan" || req.DelegationMode == "spawn" {
				if req.PlanPhase == "execution" {
					// Execution-only mode: skip planning, delegate directly
					underlyingAgent.AppendSystemPrompt(virtualtools.GetExecutionOnlyInstructions())
					log.Printf("[DELEGATION] Added execution-only instructions to system prompt")
				} else {
					// Autonomous mode: agent decides whether to plan, delegate, or do it itself
					underlyingAgent.AppendSystemPrompt(virtualtools.GetAutonomousDelegationInstructions())
					log.Printf("[DELEGATION] Added autonomous delegation instructions to system prompt (mode: %s)", req.DelegationMode)
				}
				if section := virtualtools.BuildSpawnCapabilitiesSection(buildCapabilitiesContext(req)); section != "" {
					underlyingAgent.AppendSystemPrompt(section)
				}
				// Inject custom tier descriptions into system prompt so the manager knows about them
				if delegationTierCfg := resolveDelegationTierConfig(req.DelegationTierConfig); delegationTierCfg != nil {
					if tierSection := virtualtools.BuildCustomTierPromptSection(delegationTierCfg); tierSection != "" {
						underlyingAgent.AppendSystemPrompt(tierSection)
					}
				}
			}

			// Memory tools are available in all chat modes.
			// Use per-session memory folder if set.
			// Session state (memFolderForPrompt, llmGuidance) was already read above.
			underlyingAgent.AppendSystemPrompt(virtualtools.GetMemoryInstructions(memFolderForPrompt))

			// Update code execution registry AFTER all AppendSystemPrompt calls so that
			// AppendedSystemPrompts is fully populated. rebuildSystemPromptWithUpdatedToolStructure
			// will then re-assemble the final prompt as: (clean base with tool structure) + all appended prompts.
			if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
				log.Printf("[CUSTOM TOOLS] Warning: Failed to update code execution registry: %v", err)
			}

			log.Printf("[SYSTEM_PROMPT] Final assembled prompt length=%d chars, hasGuidance=%v", len(underlyingAgent.GetSystemPrompt()), req.LLMGuidance != "" || llmGuidance != "")

			// Add CLI-specific human tool override when using CLI-based providers.
			// This covers delegation tools, memory tools, and human interaction tools —
			// all registered under the "human" category and accessible only via HTTP API.
			if req.Provider == "claude-code" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[CLAUDE CODE] Added human tool HTTP API override instructions")
			}
			if req.Provider == "gemini-cli" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[GEMINI CLI] Added human tool HTTP API override instructions")
			}
			if req.Provider == "codex-cli" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[CODEX CLI] Added human tool HTTP API override instructions")
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
			hasCamofox := false
			for _, s := range req.EnabledServers {
				if s == "playwright" {
					hasPlaywright = true
				}
				if s == "camofox" {
					hasCamofox = true
				}
			}
			if hasBrowserAccess || hasPlaywright || hasCamofox {
				// Register transformer on the agent (primary path for LLM-driven tool calls).
				// Agent tool calls go through conversation.go → toolArgTransformers, NOT through
				// the HTTP /api/mcp/execute handler. Without this, the transformer never fires.
				wsAbsPath, err := filepath.Abs("../workspace-docs/_users/default")
				if err == nil {
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

				// Get workspace path and objective from preset
				phaseWorkspacePath := ""
				phaseObjective := ""
				if req.PresetQueryID != "" {
					phaseCtx, phaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer phaseCancel()
					preset, err := api.chatDB.GetPresetQuery(phaseCtx, req.PresetQueryID)
					if err == nil && preset != nil {
						if preset.SelectedFolder.Valid && preset.SelectedFolder.String != "" {
							phaseWorkspacePath = preset.SelectedFolder.String
						}
						if preset.Query != "" {
							phaseObjective = preset.Query
						}
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
					workspace.SetSessionFolderGuard(sessionID,
						[]string{phaseWorkspacePath, "Chats", "Plans", "skills", "subagents", "Downloads"},
						[]string{phaseWorkspacePath},
					)
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
					var data virtualtools.WorkspaceFileContent
					if err := json.Unmarshal([]byte(result), &data); err != nil {
						return "", err
					}
					return data.Content, nil
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
				phaseRunFolder := ""
				var phaseEnabledGroupIDs []string
				if req.ExecutionOptions != nil {
					phaseRunFolder = req.ExecutionOptions.SelectedRunFolder
					phaseEnabledGroupIDs = req.ExecutionOptions.EnabledGroupIDs
				}
				// Builder chat uses iteration-0 as its scratch run by default.
				// Other phases keep the existing "latest iteration" fallback.
				if phaseRunFolder == "" && phaseWorkspacePath != "" {
					if workflowPhaseID == "workflow-builder" {
						phaseRunFolder = "iteration-0"
					} else {
						phaseRunFolder = resolveLatestRunFolder(context.WithoutCancel(r.Context()), phaseWorkspacePath, phaseWSClient)
					}
				}
				phaseTemplateVars := map[string]string{
					"Objective":           phaseObjective,
					"WorkspacePath":       phaseWorkspacePath,
					"IsCodeExecutionMode": "true",
				}

				// Pass workshop mode from frontend override (auto-detection happens after plan is loaded below)
				if req.ExecutionOptions != nil && req.ExecutionOptions.WorkshopMode != "" {
					phaseTemplateVars["WorkshopMode"] = req.ExecutionOptions.WorkshopMode
					log.Printf("[WORKSHOP_MODE] Using frontend override: %s", req.ExecutionOptions.WorkshopMode)
				}

				// Build GroupInfo and extra template vars for the interactive-workshop system prompt
				if workflowPhaseID == "workflow-builder" {
					groupInfo := buildWorkshopGroupInfo(r.Context(), phaseWorkspacePath, phaseReadFile, phaseRunFolder, phaseEnabledGroupIDs)
					if groupInfo != "" {
						phaseTemplateVars["GroupInfo"] = groupInfo
					}
					phaseTemplateVars["RunFolder"] = phaseRunFolder
					phaseTemplateVars["UseKnowledgebase"] = "true" // default; overridden by preset below if needed
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

				// Auto-detect workshop mode if not provided by frontend
				if phaseTemplateVars["WorkshopMode"] == "" && existingPlanJSON != "" && workflowPhaseID == "workflow-builder" {
					stepConfigJSON, _ := phaseReadFile(setupCtx, phaseWorkspacePath+"/planning/step_config.json")
					optimizedSet := make(map[string]bool)
					if stepConfigJSON != "" {
						var scData struct {
							Steps []struct {
								ID           string `json:"id"`
								AgentConfigs *struct {
									Optimized *bool `json:"optimized,omitempty"`
								} `json:"agent_configs,omitempty"`
							} `json:"steps"`
						}
						if err := json.Unmarshal([]byte(stepConfigJSON), &scData); err == nil {
							for _, sc := range scData.Steps {
								if sc.AgentConfigs != nil && sc.AgentConfigs.Optimized != nil && *sc.AgentConfigs.Optimized {
									optimizedSet[sc.ID] = true
								}
							}
						}
					}
					var planData struct {
						Steps []struct {
							ID string `json:"id"`
						} `json:"steps"`
					}
					if err := json.Unmarshal([]byte(existingPlanJSON), &planData); err == nil {
						var unoptimized []string
						for _, s := range planData.Steps {
							if !optimizedSet[s.ID] {
								unoptimized = append(unoptimized, s.ID)
							}
						}
						totalSteps := len(planData.Steps)
						optimizedCount := totalSteps - len(unoptimized)
						if optimizedCount == 0 {
							phaseTemplateVars["WorkshopMode"] = "builder"
						} else if optimizedCount >= totalSteps {
							phaseTemplateVars["WorkshopMode"] = "runner"
						} else {
							phaseTemplateVars["WorkshopMode"] = "optimizer"
						}
						if len(unoptimized) > 0 {
							phaseTemplateVars["UnoptimizedSteps"] = strings.Join(unoptimized, ", ")
						}
						log.Printf("[WORKSHOP_MODE] Auto-detected: %s (optimized: %d/%d)", phaseTemplateVars["WorkshopMode"], optimizedCount, totalSteps)
					}
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
				if req.UseCodeExecutionMode {
					codeExecInstructions := prompt.GetCodeExecutionInstructions("")
					phaseSystemPrompt += "\n\n## Code Execution Mode\n" + codeExecInstructions
				} else if req.UseToolSearchMode {
					toolSearchInstructions := prompt.GetToolSearchInstructions()
					phaseSystemPrompt += "\n\n## Tool Search Mode\n" + toolSearchInstructions
				}

				// Override the agent's system prompt — use SetSystemPrompt to properly set tracking flags
				// so that rebuildSystemPromptWithUpdatedToolStructure preserves this prompt
				underlyingAgent.ClearAppendedSystemPrompts()
				underlyingAgent.SetSystemPrompt(phaseSystemPrompt)
				log.Printf("[WORKFLOW_PHASE] Overrode system prompt (%d chars) for phase=%s", len(phaseSystemPrompt), workflowPhaseID)

				// Re-append supplementary prompts after system prompt override
				// (ClearAppendedSystemPrompts above wiped browser/GWS/secrets instructions)
				if workflowPhaseID == "workflow-builder" || workflowPhaseID == "evaluation-builder" {
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

					// Browser + GWS instructions from preset config
					// The preset determines browser mode, not req.EnableBrowserAccess (which is false for workflow_phase)
					if req.PresetQueryID != "" {
						presetCtx2, presetCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
						presetForBrowser, presetErr2 := api.chatDB.GetPresetQuery(presetCtx2, req.PresetQueryID)
						presetCancel2()
						if presetErr2 == nil && presetForBrowser != nil {
							// Resolve browser mode: prefer BrowserMode field, fall back to legacy EnableBrowserAccess
							effectiveBrowserMode := presetForBrowser.BrowserMode
							if effectiveBrowserMode == "" && presetForBrowser.EnableBrowserAccess {
								effectiveBrowserMode = "headless" // Legacy preset without browser_mode
							}

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
							case "stealth":
								phaseBrowserCfg.HasCamofox = true
							}
							// Also check selectedServers for playwright/camofox (may be set independently)
							for _, s := range selectedServers {
								switch s {
								case "playwright":
									phaseBrowserCfg.HasPlaywright = true
								case "camofox":
									phaseBrowserCfg.HasCamofox = true
								}
							}
							if phaseBrowserPrompt := browserinstructions.BuildBrowserInstructions(phaseBrowserCfg); phaseBrowserPrompt != "" {
								underlyingAgent.AppendSystemPrompt(phaseBrowserPrompt)
								log.Printf("[WORKFLOW_PHASE] Appended browser instructions to %s (mode=%s, playwright=%v, camofox=%v, agent-browser=%v)",
									workflowPhaseID, effectiveBrowserMode, phaseBrowserCfg.HasPlaywright, phaseBrowserCfg.HasCamofox, phaseBrowserCfg.HasAgentBrowser)
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

				// Register phase-appropriate tools
				switch workflowPhaseID {
				case "workflow-builder":
					// Plan modification tools + workshop execution tools (execute_step, query_step, stop_step, etc.)
					if err := todo_creation_human.RegisterPlanModificationTools(
						underlyingAgent,
						phaseWorkspacePath,
						api.logger,
						phaseReadFile,
						phaseWriteFile,
						phaseMoveFile,
						fmt.Sprintf("%s chat agent", workflowPhaseID),
					); err != nil {
						log.Printf("[WORKFLOW_PHASE] Warning: Failed to register plan modification tools for workshop: %v", err)
					} else {
						log.Printf("[WORKFLOW_PHASE] Registered plan modification tools for %s", workflowPhaseID)
					}

					// Get or create per-session workshop controller + step registry
					workshopSessionKey := sessionID
					var workshopSession *todo_creation_human.WorkshopChatSession
					if cached, ok := api.workshopChatSessions.Load(workshopSessionKey); ok {
						workshopSession = cached.(*todo_creation_human.WorkshopChatSession)
						log.Printf("[WORKFLOW_PHASE] Reusing existing workshop session for %s", sessionID)

						// Refresh enabled group IDs from current request (toolbar selection may have changed)
						if req.ExecutionOptions != nil && len(req.ExecutionOptions.EnabledGroupIDs) > 0 {
							workshopSession.UpdateEnabledGroupIDs(r.Context(), req.ExecutionOptions.EnabledGroupIDs)
							log.Printf("[WORKFLOW_PHASE] Refreshed enabled group IDs: %v", req.ExecutionOptions.EnabledGroupIDs)
						}

						// Refresh all preset settings from database in case user edited the workflow
						if req.PresetQueryID != "" {
							refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 5*time.Second)
							refreshPreset, refreshErr := api.chatDB.GetPresetQuery(refreshCtx, req.PresetQueryID)
							refreshCancel()
							if refreshErr != nil {
								log.Printf("[WORKFLOW_PHASE] Warning: Failed to reload preset config: %v", refreshErr)
							} else if refreshPreset != nil {
								// Refresh non-LLM settings from preset (only apply if parse succeeds)
								var refreshedTools []string
								toolsParsed := refreshPreset.SelectedTools == "" // empty = no tools, valid
								if refreshPreset.SelectedTools != "" {
									if err := json.Unmarshal([]byte(refreshPreset.SelectedTools), &refreshedTools); err != nil {
										log.Printf("[WORKFLOW_PHASE] Warning: Failed to parse refreshed tools: %v — keeping existing", err)
									} else {
										toolsParsed = true
									}
								}
								var refreshedPreDiscovered []string
								preDiscoveredParsed := refreshPreset.PreDiscoveredTools == ""
								if refreshPreset.PreDiscoveredTools != "" {
									if err := json.Unmarshal([]byte(refreshPreset.PreDiscoveredTools), &refreshedPreDiscovered); err != nil {
										log.Printf("[WORKFLOW_PHASE] Warning: Failed to parse refreshed pre-discovered tools: %v — keeping existing", err)
									} else {
										preDiscoveredParsed = true
									}
								}
								var refreshedSkills []string
								skillsParsed := refreshPreset.SelectedSkills == ""
								if refreshPreset.SelectedSkills != "" {
									if err := json.Unmarshal([]byte(refreshPreset.SelectedSkills), &refreshedSkills); err != nil {
										log.Printf("[WORKFLOW_PHASE] Warning: Failed to parse refreshed skills: %v — keeping existing", err)
									} else {
										skillsParsed = true
									}
								}

								// Refresh secrets
								var refreshedSecretNames *[]string
								if refreshPreset.SelectedGlobalSecretNames != "" {
									var names []string
									if err := json.Unmarshal([]byte(refreshPreset.SelectedGlobalSecretNames), &names); err == nil {
										refreshedSecretNames = &names
									}
								} else {
									emptyNames := []string{}
									refreshedSecretNames = &emptyNames
								}
								effectiveSecretSelection := req.SelectedGlobalSecrets
								if refreshedSecretNames != nil {
									effectiveSecretSelection = refreshedSecretNames
								}
								allRefreshedSecrets := mergeGlobalSecrets(req.DecryptedSecrets, effectiveSecretSelection)
								var secretEntries []orchestrator.SecretEntry
								for _, s := range allRefreshedSecrets {
									secretEntries = append(secretEntries, orchestrator.SecretEntry{Name: s.Name, Value: s.Value})
								}

								// Determine knowledgebase setting from LLM config block
								refreshedKnowledgebase := true // default
								if len(refreshPreset.LLMConfig) > 0 {
									var presetLLMConfig database.PresetLLMConfig
									if jsonErr := json.Unmarshal(refreshPreset.LLMConfig, &presetLLMConfig); jsonErr == nil {
										// Refresh LLM configs
										phaseLLM := workshopExtractLLM(presetLLMConfig.PhaseLLM, presetLLMConfig.Provider, presetLLMConfig.ModelID)
										workshopSession.UpdatePresetLLMConfigs(phaseLLM)
										log.Printf("[WORKFLOW_PHASE] Refreshed phase LLM config: %v", phaseLLM != nil)

										// Refresh tiered LLM allocation config
										if presetLLMConfig.TieredConfig != nil {
											refreshedTiered := &todo_creation_human.TieredLLMConfig{
												Tier1: &todo_creation_human.AgentLLMConfig{
													Provider:  presetLLMConfig.TieredConfig.Tier1.Provider,
													ModelID:   presetLLMConfig.TieredConfig.Tier1.ModelID,
													Fallbacks: workshopConvertFallbacks(presetLLMConfig.TieredConfig.Tier1.Fallbacks),
												},
												Tier2: &todo_creation_human.AgentLLMConfig{
													Provider:  presetLLMConfig.TieredConfig.Tier2.Provider,
													ModelID:   presetLLMConfig.TieredConfig.Tier2.ModelID,
													Fallbacks: workshopConvertFallbacks(presetLLMConfig.TieredConfig.Tier2.Fallbacks),
												},
												Tier3: &todo_creation_human.AgentLLMConfig{
													Provider:  presetLLMConfig.TieredConfig.Tier3.Provider,
													ModelID:   presetLLMConfig.TieredConfig.Tier3.ModelID,
													Fallbacks: workshopConvertFallbacks(presetLLMConfig.TieredConfig.Tier3.Fallbacks),
												},
											}
											workshopSession.UpdateTieredConfig(refreshedTiered)
											log.Printf("[WORKFLOW_PHASE] Refreshed tiered config: T1=%s/%s T2=%s/%s T3=%s/%s",
												refreshedTiered.Tier1.Provider, refreshedTiered.Tier1.ModelID,
												refreshedTiered.Tier2.Provider, refreshedTiered.Tier2.ModelID,
												refreshedTiered.Tier3.Provider, refreshedTiered.Tier3.ModelID)
										} else {
											workshopSession.UpdateTieredConfig(nil)
											log.Printf("[WORKFLOW_PHASE] Tiered config not present")
										}

										if presetLLMConfig.UseKnowledgebase != nil {
											refreshedKnowledgebase = *presetLLMConfig.UseKnowledgebase
										}
									} else {
										log.Printf("[WORKFLOW_PHASE] Warning: Failed to parse refreshed LLM config: %v — keeping existing", jsonErr)
									}
								}

								// Apply settings (only update fields that parsed successfully)
								workshopSession.UpdatePresetSettings(
									selectedServers,
									refreshedTools, toolsParsed,
									refreshPreset.UseCodeExecutionMode,
									refreshPreset.UseToolSearchMode,
									refreshedPreDiscovered, preDiscoveredParsed,
									refreshedKnowledgebase,
									refreshedSkills, skillsParsed,
									secretEntries,
								)
								log.Printf("[WORKFLOW_PHASE] Refreshed preset settings: servers=%d tools=%d codeExec=%v toolSearch=%v kb=%v skills=%d secrets=%d",
									len(selectedServers), len(refreshedTools), refreshPreset.UseCodeExecutionMode,
									refreshPreset.UseToolSearchMode, refreshedKnowledgebase, len(refreshedSkills), len(secretEntries))
							}
						}
					} else {
						// Build full workshop config matching normal workflow setup
						workshopCfg := api.buildWorkshopConfig(r.Context(), req, currentUserID, phaseWorkspacePath, phaseRunFolder, selectedServers, sessionID)

						newSession, sessionErr := todo_creation_human.NewWorkshopChatSession(r.Context(), workshopCfg)
						if sessionErr != nil {
							log.Printf("[WORKFLOW_PHASE] Warning: Failed to create workshop session for %s: %v — workshop execution tools unavailable", workflowPhaseID, sessionErr)
						} else {
							workshopSession = newSession
							api.workshopChatSessions.Store(workshopSessionKey, workshopSession)
							log.Printf("[WORKFLOW_PHASE] Created new %s session for %s", workflowPhaseID, sessionID)
						}
					}

					if workshopSession != nil {
						// Wire bgAgentRegistry notifier so todo task sub-agents auto-notify the main agent.
						// Called every request (safe: always rebuilds the chain, no duplicates).
						workshopSession.SetExtraSubAgentNotifier(&todoSubAgentBgNotifier{api: api, sessionID: sessionID})
						todo_creation_human.RegisterWorkshopChatTools(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered workshop execution tools for %s (execute_step, query_step, stop_step, list_steps, etc.)", workflowPhaseID)
					}

					// Register evaluation tools in builder-style phases (eval plan validation + run_full_evaluation)
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

					if err := todo_creation_human.RegisterOutputModificationTools(
						underlyingAgent,
						phaseWorkspacePath,
						api.logger,
						phaseReadFile,
						phaseWriteFile,
						phaseMoveFile,
					); err != nil {
						log.Printf("[WORKFLOW_PHASE] Warning: Failed to register output modification tools in %s: %v", workflowPhaseID, err)
					} else {
						log.Printf("[WORKFLOW_PHASE] Registered output modification tools in %s", workflowPhaseID)
					}

					// Create eval session for run_full_evaluation (needs isEvaluationMode=true)
					evalSessionKey := "eval-" + sessionID
					var evalSession *todo_creation_human.WorkshopChatSession
					if cached, ok := api.workshopChatSessions.Load(evalSessionKey); ok {
						evalSession = cached.(*todo_creation_human.WorkshopChatSession)
						log.Printf("[WORKFLOW_PHASE] Reusing existing eval session in %s %s", workflowPhaseID, sessionID)
					} else {
						evalCfg := api.buildWorkshopConfig(r.Context(), req, currentUserID, phaseWorkspacePath, phaseRunFolder, selectedServers, sessionID)
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
					if evalSession != nil {
						todo_creation_human.RegisterRunFullEvaluationTool(underlyingAgent, evalSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered run_full_evaluation in %s", workflowPhaseID)
					}
					if workshopSession != nil {
						todo_creation_human.RegisterRunFullReportTool(underlyingAgent, workshopSession, api.logger)
						log.Printf("[WORKFLOW_PHASE] Registered run_full_report in %s", workflowPhaseID)
					}
				default:
					// planning: plan modification tools
					if err := todo_creation_human.RegisterPlanModificationTools(
						underlyingAgent,
						phaseWorkspacePath,
						api.logger,
						phaseReadFile,
						phaseWriteFile,
						phaseMoveFile,
						fmt.Sprintf("%s chat agent", workflowPhaseID),
					); err != nil {
						log.Printf("[WORKFLOW_PHASE] Warning: Failed to register plan modification tools: %v", err)
					} else {
						log.Printf("[WORKFLOW_PHASE] Registered plan modification tools for phase=%s", workflowPhaseID)
					}
				}

				// Rebuild code execution registry after prompt + tool changes
				if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
					log.Printf("[WORKFLOW_PHASE] Warning: Failed to update code execution registry: %v", err)
				}

				log.Printf("[WORKFLOW_PHASE] Phase chat setup complete: phase=%s workspace=%s", workflowPhaseID, phaseWorkspacePath)
			}
		}

		// Add event observer immediately after agent creation to capture all events
		// ✅ FIX: Always attach EventObserver to agent, even in orchestrator mode
		// The eventbridge.OrchestratorAgentEventBridge handles orchestrator-specific events, but we still need EventObserver for regular agent events
		log.Printf("[DATABASE DEBUG] Starting event observer setup for session %s", sessionID)
		log.Printf("[DATABASE DEBUG] ChatDB available: %v", api.chatDB != nil)

		log.Printf("[DATABASE DEBUG] Creating in-memory event observer for session %s", sessionID)
		// Create in-memory event observer for real-time updates
		eventObserver := events.NewEventObserverWithLogger(api.eventStore, sessionID, api.logger)

		log.Printf("[DATABASE DEBUG] Creating database event observer for session %s", sessionID)
		// Create database event observer to store events in database
		dbEventObserver := database.NewEventDatabaseObserver(api.chatDB, func(eventType string) bool {
			return events.ShouldShowEvent(eventType)
		})
		log.Printf("[DATABASE DEBUG] Database event observer created successfully for session %s", sessionID)

		// Add event observer directly to the underlying MCP agent since the wrapper's AddEventListener is disabled
		log.Printf("[DATABASE DEBUG] Getting underlying agent for session %s", sessionID)
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			log.Printf("[DATABASE DEBUG] Underlying agent found, adding event observers for session %s", sessionID)
			underlyingAgent.AddEventListener(eventObserver)
			log.Printf("[DATABASE DEBUG] Added in-memory event observer for session %s", sessionID)
			underlyingAgent.AddEventListener(dbEventObserver)
			log.Printf("[DATABASE DEBUG] Added database event observer for session %s", sessionID)

			// Store running agent reference for steer message injection
			api.runningAgentsMux.Lock()
			api.runningAgents[sessionID] = underlyingAgent
			api.runningAgentsMux.Unlock()
		} else {
			log.Printf("[DATABASE DEBUG] ERROR: Underlying MCP agent is nil for session %s", sessionID)
		}

		// --- BEGIN: Load conversation history and accumulate for streaming ---
		// Load conversation history for this session
		api.conversationMux.RLock()
		history, exists := api.conversationHistory[sessionID]
		api.conversationMux.RUnlock()

		if exists && len(history) > 0 {
			log.Printf("[CONVERSATION DEBUG] Loading %d messages from in-memory conversation history for session %s", len(history), sessionID)
			// Load the conversation history into the agent
			for _, msg := range history {
				llmAgent.AppendMessage(msg)
			}
		} else if api.chatDB != nil {
			// Try to load conversation history from database
			log.Printf("[CONVERSATION DEBUG] No in-memory history found for session %s, attempting to load from database", sessionID)

			// Get events for this session, focusing on conversation_turn events
			dbEvents, err := api.chatDB.GetEventsBySession(r.Context(), sessionID, 1000, 0)
			if err != nil {
				log.Printf("[CONVERSATION DEBUG] Failed to load events from database for session %s: %v", sessionID, err)
			} else {
				// Find the last conversation_turn event which contains full conversation history
				var lastTurnEvent *database.Event
				for i := len(dbEvents) - 1; i >= 0; i-- {
					if dbEvents[i].EventType == string(unifiedevents.ConversationTurn) {
						lastTurnEvent = &dbEvents[i]
						break
					}
				}

				if lastTurnEvent != nil {
					// Parse the event data using typed structures
					// EventData contains the full AgentEvent JSON structure
					// Use a helper struct to unmarshal Data as json.RawMessage first
					type agentEventHelper struct {
						Type unifiedevents.EventType `json:"type"`
						Data json.RawMessage         `json:"data"`
					}

					var agentEvent agentEventHelper
					if err := json.Unmarshal(lastTurnEvent.EventData, &agentEvent); err != nil {
						log.Printf("[CONVERSATION DEBUG] Failed to parse AgentEvent for session %s: %v", sessionID, err)
					} else if agentEvent.Type == unifiedevents.ConversationTurn {
						// Unmarshal Data field to ConversationTurnEvent
						var turnEvent unifiedevents.ConversationTurnEvent
						if err := json.Unmarshal(agentEvent.Data, &turnEvent); err != nil {
							log.Printf("[CONVERSATION DEBUG] Failed to parse ConversationTurnEvent data for session %s: %v", sessionID, err)
						} else if len(turnEvent.Messages) > 0 {
							// Deserialize messages from SerializedMessage format back to llmtypes.MessageContent
							deserializedHistory := make([]llmtypes.MessageContent, 0, len(turnEvent.Messages))
							for _, serializedMsg := range turnEvent.Messages {
								msg := deserializeSerializedMessage(serializedMsg)
								if msg != nil {
									deserializedHistory = append(deserializedHistory, *msg)
								}
							}

							if len(deserializedHistory) > 0 {
								log.Printf("[CONVERSATION DEBUG] Loaded %d messages from database conversation history for session %s", len(deserializedHistory), sessionID)
								// Load the conversation history into the agent
								for _, msg := range deserializedHistory {
									llmAgent.AppendMessage(msg)
								}
								// Cache in memory for future use
								api.conversationMux.Lock()
								api.conversationHistory[sessionID] = deserializedHistory
								api.conversationMux.Unlock()
							} else {
								log.Printf("[CONVERSATION DEBUG] No valid messages found after deserialization for session %s", sessionID)
							}
						} else {
							log.Printf("[CONVERSATION DEBUG] ConversationTurnEvent has no messages for session %s", sessionID)
						}
					} else {
						log.Printf("[CONVERSATION DEBUG] Event type is %s, expected conversation_turn for session %s", agentEvent.Type, sessionID)
					}
				} else {
					log.Printf("[CONVERSATION DEBUG] No conversation_turn event found in database for session %s", sessionID)
				}
			}
		} else {
			log.Printf("[CONVERSATION DEBUG] No conversation history found for session %s, starting fresh", sessionID)
		}

		// Note: User message is added by StreamWithEvents internally, no need to add it here
		// --- END: Load conversation history and accumulate for streaming ---

		log.Printf("[AGENT DEBUG] Starting agent processing for query %s", queryID)

		// Create a cancellable context for agent execution using background context
		// This prevents the agent from being canceled when the HTTP request ends
		agentCtx, agentCancel := context.WithCancel(context.Background())

		// Inject user ID into the agent context for per-user folder isolation
		// This allows workspace tools to route per-user folders correctly
		agentCtx = context.WithValue(agentCtx, common.UserIDKey, currentUserID)
		agentCtx = context.WithValue(agentCtx, common.ChatSessionIDKey, sessionID)
		log.Printf("[USER_ID_DEBUGGING] Main agent: injected UserIDKey=%q, ChatSessionIDKey=%q into agentCtx", currentUserID, sessionID)

		// Store the cancel function for potential cancellation
		api.agentCancelMux.Lock()
		api.agentCancelFuncs[sessionID] = agentCancel
		api.agentCancelMux.Unlock()

		// Merge global secrets with user-supplied secrets, then inject into system prompt (not user message)
		chatQuery := req.Query


		allChatSecrets := mergeGlobalSecrets(req.DecryptedSecrets, req.SelectedGlobalSecrets)
		if len(allChatSecrets) > 0 {
			var secretParts []string
			for _, s := range allChatSecrets {
				secretParts = append(secretParts, fmt.Sprintf("### %s\n```\n%s\n```", s.Name, s.Value))
			}
			secretPrompt := "\n## 🔐 Secrets\n\nThe following secrets/credentials have been provided. They are also available as environment variables in execute_shell_command with a SECRET_ prefix (e.g., os.environ[\"SECRET_MY_KEY\"] in Python or $SECRET_MY_KEY in bash).\n\n" + strings.Join(secretParts, "\n")
			if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
				underlyingAgent.AppendSystemPrompt(secretPrompt)
				log.Printf("[SECRETS] Injected %d secrets (%d global + %d user) into system prompt", len(allChatSecrets), len(allChatSecrets)-len(req.DecryptedSecrets), len(req.DecryptedSecrets))
			}
			// Inject secrets as environment variables for shell execution (SECRET_ prefix for whitelist)
			if workspaceEnv != nil {
				for _, s := range allChatSecrets {
					workspaceEnv["SECRET_"+s.Name] = s.Value
				}
				log.Printf("[SECRETS] Injected %d secrets as environment variables for shell execution", len(allChatSecrets))
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

		// Use the enhanced wrapper to get text chunks - events are handled via EventObserver and polling API
		log.Printf("[STREAMING_LIFECYCLE] T+%dms | Starting StreamWithEvents | session=%s query=%.80s", time.Since(startTime).Milliseconds(), sessionID, chatQuery)
		textChan, err := llmAgent.StreamWithEvents(agentCtx, chatQuery)
		if err != nil {
			log.Printf("[AGENT DEBUG] llmAgent.StreamWithEvents() error: %v", err)
			sendError(fmt.Sprintf("Failed to start streaming: %v", err), true)
			return
		}
		log.Printf("[LATENCY_DEBUG] T+%dms | StreamWithEvents channel opened | queryID=%s", time.Since(startTime).Milliseconds(), queryID)
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

				// Update chat session status to error for timeout
				if chatSession != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Title:     chatSession.Title,     // Preserve existing title
						AgentMode: chatSession.AgentMode, // Preserve existing agent_mode
						Status:    "error",
					}
					_, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (timeout): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status (timeout)", sessionID)
					}
				}

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
		// This ensures we capture the final state even if streaming was interrupted
		finalHistory := llmAgent.GetHistory()
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = finalHistory
		api.conversationMux.Unlock()
		log.Printf("[CONVERSATION DEBUG] Final save: %d messages to conversation history for session %s", len(finalHistory), sessionID)

		// Persist conversation history to DB so follow-up queries can restore it after server restart.
		// We store a single conversation_turn event with the full message history — this is only
		// written once at the end of each query (not per-turn) to avoid bloating the DB.
		if api.chatDB != nil && len(finalHistory) > 0 {
			turnEvent := unifiedevents.NewConversationTurnEvent(
				0,  // turn number not critical for restoration
				"", // question not needed
				len(finalHistory),
				false, 0, nil,
				finalHistory,
			)
			agentEvent := &unifiedevents.AgentEvent{
				Type:      unifiedevents.ConversationTurn,
				Timestamp: time.Now(),
				SessionID: sessionID,
				Data:      turnEvent,
			}
			if err := api.chatDB.StoreEvent(context.Background(), sessionID, agentEvent); err != nil {
				log.Printf("[CONVERSATION DEBUG] Failed to persist conversation history to DB for session %s: %v", sessionID, err)
			} else {
				log.Printf("[CONVERSATION DEBUG] Persisted conversation history (%d messages) to DB for session %s", len(finalHistory), sessionID)
			}
		}

		// Save builder conversation log + token_usage.json for workflow phase sessions.
		// One file per session — overwrites on each follow-up with the full cumulative history.
		// Resolve workspace-docs root so files are visible in the UI.
		if isWorkflowPhase && workflowPhaseFolder != "" && len(finalHistory) > 0 {
			wsRoot := filepath.Join("..", "workspace-docs")
			builderDir := filepath.Join(wsRoot, workflowPhaseFolder, "builder")
			if err := os.MkdirAll(builderDir, 0755); err != nil {
				log.Printf("[BUILDER LOG] Failed to create builder dir %s: %v", builderDir, err)
			} else {
				// 1. Save conversation log (one file per session, overwritten on each turn)
				convData := map[string]interface{}{
					"session_id":           sessionID,
					"phase_id":             workflowPhaseID,
					"conversation_history": finalHistory,
					"updated_at":           time.Now().Format(time.RFC3339),
				}
				if convJSON, err := json.MarshalIndent(convData, "", "  "); err == nil {
					logPath := filepath.Join(builderDir, fmt.Sprintf("session-%s-conversation.json", sessionID))
					if err := os.WriteFile(logPath, convJSON, 0644); err != nil {
						log.Printf("[BUILDER LOG] Failed to write conversation log: %v", err)
					} else {
						log.Printf("[BUILDER LOG] Saved conversation log (%d messages) to %s", len(finalHistory), logPath)
					}
				}

				// 2. Update {workflowFolder}/token_usage.json (same PhaseTokenUsageFile format used by execution)
				// This adds builder costs alongside execution phase costs in the same file,
				// keyed as "builder" phase so it appears in the existing cost popup.
				if underlying := llmAgent.GetUnderlyingAgent(); underlying != nil {
					promptTokens, completionTokens, _, cacheTokens, reasoningTokens, llmCallCount, _,
						inputCost, outputCost, reasoningCost, cacheCost, totalCost, _ := underlying.GetTokenUsageWithPricing()

					fmtM := func(tokens int) string {
						return fmt.Sprintf("%.3fM", float64(tokens)/1_000_000.0)
					}

					phaseKey := workflowPhaseID // e.g. "workflow-builder", "planning"
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

					tokenFilePath := filepath.Join(wsRoot, workflowPhaseFolder, "token_usage.json")
					var tokenFile orchestrator.PhaseTokenUsageFile
					if existingData, err := os.ReadFile(tokenFilePath); err == nil {
						json.Unmarshal(existingData, &tokenFile)
					}
					if tokenFile.ByPhaseAndModel == nil {
						tokenFile.ByPhaseAndModel = make(map[string]map[string]*orchestrator.ModelTokenUsage)
						tokenFile.ByModel = make(map[string]*orchestrator.ModelTokenUsage)
						tokenFile.CreatedAt = time.Now()
					}
					tokenFile.UpdatedAt = time.Now()

					// Add/accumulate per-phase entry
					if tokenFile.ByPhaseAndModel[phaseKey] == nil {
						tokenFile.ByPhaseAndModel[phaseKey] = make(map[string]*orchestrator.ModelTokenUsage)
					}
					if existing, ok := tokenFile.ByPhaseAndModel[phaseKey][underlying.ModelID]; ok {
						existing.InputTokens += promptTokens
						existing.OutputTokens += completionTokens
						existing.CacheTokens += cacheTokens
						existing.ReasoningTokens += reasoningTokens
						existing.LLMCallCount += llmCallCount
						existing.InputTokensM = fmtM(existing.InputTokens)
						existing.OutputTokensM = fmtM(existing.OutputTokens)
						existing.CacheTokensM = fmtM(existing.CacheTokens)
						existing.ReasoningTokensM = fmtM(existing.ReasoningTokens)
						existing.InputCost += inputCost
						existing.OutputCost += outputCost
						existing.ReasoningCost += reasoningCost
						existing.CacheCost += cacheCost
						existing.TotalCost += totalCost
					} else {
						tokenFile.ByPhaseAndModel[phaseKey][underlying.ModelID] = modelUsage
					}

					// Update aggregate by_model
					if existing, ok := tokenFile.ByModel[underlying.ModelID]; ok {
						existing.InputTokens += promptTokens
						existing.OutputTokens += completionTokens
						existing.CacheTokens += cacheTokens
						existing.ReasoningTokens += reasoningTokens
						existing.LLMCallCount += llmCallCount
						existing.InputTokensM = fmtM(existing.InputTokens)
						existing.OutputTokensM = fmtM(existing.OutputTokens)
						existing.CacheTokensM = fmtM(existing.CacheTokens)
						existing.ReasoningTokensM = fmtM(existing.ReasoningTokens)
						existing.InputCost += inputCost
						existing.OutputCost += outputCost
						existing.ReasoningCost += reasoningCost
						existing.CacheCost += cacheCost
						existing.TotalCost += totalCost
					} else {
						tokenFile.ByModel[underlying.ModelID] = modelUsage
					}

					if tokenJSON, err := json.MarshalIndent(tokenFile, "", "  "); err == nil {
						if err := os.WriteFile(tokenFilePath, tokenJSON, 0644); err != nil {
							log.Printf("[BUILDER LOG] Failed to write token_usage.json: %v", err)
						} else {
							log.Printf("[BUILDER LOG] Updated %s/token_usage.json (phase=%s, $%.4f this turn)", workflowPhaseFolder, phaseKey, totalCost)
						}
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

		// Store agent for reuse by synthetic turns (plan mode only)
		// The stored agent retains all tools, prompts, observers, and conversation history
		if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
			api.sessionAgentsMux.Lock()
			api.sessionAgents[sessionID] = llmAgent
			api.sessionAgentsMux.Unlock()
			log.Printf("[BG AGENT] Stored agent for session %s for synthetic turn reuse", sessionID)
		}

		// Clean up the agent cancel function when streaming is complete
		api.agentCancelMux.Lock()
		delete(api.agentCancelFuncs, sessionID)
		api.agentCancelMux.Unlock()

		// --- BEGIN: Persist plan state for session resume ---
		if req.DelegationMode == "plan" || req.AgentMode == "workflow_phase" {
			planState := api.getOrCreatePlanSessionState(sessionID)
			if planState.PlanID != "" {
				// Read existing config, merge plan state, and save
				if chatSession != nil {
					existingConfig := &database.ChatSessionConfig{}
					if cs, err := api.chatDB.GetChatSession(context.Background(), sessionID); err == nil && cs != nil {
						if cfg, err := cs.GetConfig(); err == nil && cfg != nil {
							existingConfig = cfg
						}
					}
					existingConfig.PlanID = planState.PlanID
					existingConfig.PlanFolder = planState.PlanFolder
					existingConfig.PlanPhase = planState.GetPhase()
					if configJSON, err := existingConfig.ToJSON(); err == nil {
						if _, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, &database.UpdateChatSessionRequest{
							Config: configJSON,
						}); err != nil {
							log.Printf("[PLAN STATE] Failed to persist plan state: %v", err)
						} else {
							log.Printf("[PLAN STATE] Saved plan state for session %s: plan=%s phase=%s", sessionID, planState.PlanID, planState.GetPhase())
						}
					}
				}
			}
		}
		// --- END: Persist plan state for session resume ---

		// --- BEGIN: Update chat session status to completed ---
		if chatSession != nil {
			// Update session status to completed with completion timestamp
			completedAt := time.Now()
			updateReq := &database.UpdateChatSessionRequest{
				Status:      "completed",
				CompletedAt: &completedAt,
			}
			_, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
			if err != nil {
				log.Printf("[DATABASE DEBUG] Failed to update chat session status to completed: %v", err)
			} else {
				log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to completed status", sessionID)
			}
		}
		// --- END: Update chat session status to completed ---

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

// Add endpoint to stop/clear a session
func (api *StreamingAPI) handleStopSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Verify session ownership for multi-user isolation
	currentUserID := GetUserIDFromContext(r.Context())
	_, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
	if err != nil {
		http.Error(w, "Session not found or access denied", http.StatusNotFound)
		return
	}

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

	// Cancel background agents if explicitly requested (e.g. user pressed the stop button).
	// When called before sending a new message, cancelAgents is NOT set so agents survive
	// across turns and synthetic turns can still fire when they complete.
	if r.URL.Query().Get("cancelAgents") == "true" {
		api.bgAgentRegistry.CancelAll(sessionID)
		log.Printf("[SESSION DEBUG] Canceled all background agents for session %s", sessionID)
	}

	// Close workshop chat sessions for this session — cancels all running step executions.
	// Workshop sessions use context.Background() so they survive agent context cancellation above;
	// we must explicitly call Close() to cancel their step goroutines.
	// Multiple keys may exist per session: sessionID, "eval-" + sessionID.
	workshopKeys := []string{sessionID, "eval-" + sessionID}
	for _, wsKey := range workshopKeys {
		if cached, ok := api.workshopChatSessions.Load(wsKey); ok {
			if ws, ok := cached.(interface{ Close() }); ok {
				ws.Close()
				log.Printf("[SESSION DEBUG] Closed workshop session %q (all step executions cancelled)", wsKey)
			}
			api.workshopChatSessions.Delete(wsKey)
		}
	}

	// Cancel all workflow orchestrator contexts for this session
	// Since we now use queryID as the key, we need to look up all queryIDs for this session
	api.sessionQueryIDMux.Lock()
	queryIDs := api.sessionQueryIDs[sessionID]
	delete(api.sessionQueryIDs, sessionID) // Clear the mapping
	api.sessionQueryIDMux.Unlock()

	if len(queryIDs) > 0 {
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

	// Note: Conversation history and orchestrator state are preserved to allow resuming the conversation
	// Use /api/session/clear if you want to clear conversation history

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Session stopped (conversation history and orchestrator state preserved)"))
}

// Add endpoint to clear conversation history for a session
func (api *StreamingAPI) handleClearSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Verify session ownership for multi-user isolation
	currentUserID := GetUserIDFromContext(r.Context())
	_, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
	if err != nil {
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

// Chat History API Handlers

// createChatSessionHandler creates a new chat session
func createChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req database.CreateChatSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Get current user ID for session isolation
		userID := GetUserIDFromContext(r.Context())

		session, err := db.CreateChatSessionWithUser(r.Context(), &req, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(session)
	}
}

// listChatSessionsHandler lists all chat sessions with pagination
func listChatSessionsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")
		presetQueryID := r.URL.Query().Get("preset_query_id")
		agentMode := r.URL.Query().Get("agent_mode")

		limit := 20
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				offset = o
			}
		}

		// Convert preset_query_id to pointer for optional filtering
		var presetQueryIDPtr *string
		if presetQueryID != "" {
			presetQueryIDPtr = &presetQueryID
		}

		// Convert agent_mode to pointer for optional filtering
		var agentModePtr *string
		if agentMode != "" {
			agentModePtr = &agentMode
		}

		// Get current user ID for session isolation
		userID := GetUserIDFromContext(r.Context())

		sessions, total, err := db.ListChatSessionsWithUser(r.Context(), limit, offset, presetQueryIDPtr, agentModePtr, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"sessions": sessions,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getChatSessionHandler gets a specific chat session
func getChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		// Get current user ID for session isolation
		userID := GetUserIDFromContext(r.Context())

		session, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}
}

// updateChatSessionHandler updates a chat session
func updateChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		// Get current user ID for session isolation - verify ownership first
		userID := GetUserIDFromContext(r.Context())
		_, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "Session not found or access denied", http.StatusNotFound)
			return
		}

		var req database.UpdateChatSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		session, err := db.UpdateChatSession(r.Context(), sessionID, &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session)
	}
}

// deleteChatSessionHandler deletes a chat session
func deleteChatSessionHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		// Get current user ID for session isolation - verify ownership first
		userID := GetUserIDFromContext(r.Context())
		_, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "Session not found or access denied", http.StatusNotFound)
			return
		}

		err = db.DeleteChatSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Chat session deleted successfully"})
	}
}

// convertDBEventToPollingEvent converts a database.Event to events.Event format
// This is the same conversion used by the polling API to ensure consistency
func convertDBEventToPollingEvent(dbEvent database.Event, sessionID string) (*events.Event, error) {
	// Unmarshal the full AgentEvent - use helper struct to handle EventData interface
	type agentEventWithRawData struct {
		Type           unifiedevents.EventType `json:"type"`
		Timestamp      time.Time               `json:"timestamp"`
		EventIndex     int                     `json:"event_index"`
		TraceID        string                  `json:"trace_id,omitempty"`
		SpanID         string                  `json:"span_id,omitempty"`
		ParentID       string                  `json:"parent_id,omitempty"`
		CorrelationID  string                  `json:"correlation_id,omitempty"`
		HierarchyLevel int                     `json:"hierarchy_level"`
		SessionID      string                  `json:"session_id,omitempty"`
		Component      string                  `json:"component,omitempty"`
		Data           json.RawMessage         `json:"data"`
	}

	var helper agentEventWithRawData
	if err := json.Unmarshal(dbEvent.EventData, &helper); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	// Unmarshal Data field into a map to preserve structure
	var dataMap map[string]interface{}
	if err := json.Unmarshal(helper.Data, &dataMap); err != nil {
		return nil, fmt.Errorf("failed to parse event data: %w", err)
	}

	// Extract event-specific fields, excluding BaseEventData fields
	// BaseEventData fields are: timestamp, trace_id, span_id, event_id, parent_id,
	// is_end_event, correlation_id, hierarchy_level, session_id, component, metadata
	baseEventDataFields := map[string]bool{
		"timestamp":       true,
		"trace_id":        true,
		"span_id":         true,
		"event_id":        true,
		"parent_id":       true,
		"is_end_event":    true,
		"correlation_id":  true,
		"hierarchy_level": true,
		"session_id":      true,
		"component":       true,
		"metadata":        true,
	}

	actualEventData := make(map[string]interface{})
	for k, v := range dataMap {
		// Skip BaseEventData fields - they're already in AgentEvent
		if !baseEventDataFields[k] {
			actualEventData[k] = v
		}
	}

	// Create AgentEvent with flatEventData that serializes directly as event-specific fields
	// This ensures event.data.data contains the actual event data (like {content: "..."})
	agentEvent := unifiedevents.AgentEvent{
		Type:           helper.Type,
		Timestamp:      helper.Timestamp,
		EventIndex:     helper.EventIndex,
		TraceID:        helper.TraceID,
		SpanID:         helper.SpanID,
		ParentID:       helper.ParentID,
		CorrelationID:  helper.CorrelationID,
		HierarchyLevel: helper.HierarchyLevel,
		SessionID:      helper.SessionID,
		Component:      helper.Component,
		Data: &flatEventData{
			eventData: actualEventData,
			eventType: helper.Type,
		},
	}

	return &events.Event{
		ID:        dbEvent.ID,
		Type:      dbEvent.EventType,
		Timestamp: dbEvent.Timestamp,
		SessionID: sessionID,
		Data:      &agentEvent,
	}, nil
}

// getSessionEventsHandler gets events for a specific session
// Returns events in the same format as polling API (events.Event[])
func getSessionEventsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 100
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				offset = o
			}
		}

		// Cap limit to prevent slow queries and large responses (max 500 events per request)
		const maxLimit = 500
		if limit <= 0 || limit > maxLimit {
			limit = maxLimit
		}

		dbEvents, err := db.GetEventsBySession(r.Context(), sessionID, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[CHAT_HISTORY] Loading events for session %s: found %d events", sessionID, len(dbEvents))

		// Convert database events to polling events format using shared helper
		convertedEvents := make([]events.Event, 0, len(dbEvents))
		parseErrors := 0
		for i, dbEvent := range dbEvents {
			convertedEvent, err := convertDBEventToPollingEvent(dbEvent, sessionID)
			if err != nil {
				parseErrors++
				if i < 3 {
					log.Printf("[CHAT_HISTORY ERROR] Failed to convert event %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
				}
				continue
			}

			convertedEvents = append(convertedEvents, *convertedEvent)
		}

		log.Printf("[CHAT_HISTORY] Converted %d events: converted=%d, parse_errors=%d", len(dbEvents), len(convertedEvents), parseErrors)

		// Get total count using COUNT(*) - O(1) with index, avoids fetching all events
		total := offset + len(dbEvents)
		if count, err := db.CountEventsBySession(r.Context(), sessionID); err == nil {
			total = count
		}

		response := map[string]interface{}{
			"events": convertedEvents,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// searchEventsHandler searches events with filters
func searchEventsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var filter database.EventFilter

		// Parse query parameters
		if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
			filter.SessionID = sessionID
		}

		if eventType := r.URL.Query().Get("event_type"); eventType != "" {
			filter.EventType = unifiedevents.EventType(eventType)
		}

		if fromDateStr := r.URL.Query().Get("from_date"); fromDateStr != "" {
			if fromDate, err := time.Parse(time.RFC3339, fromDateStr); err == nil {
				filter.FromDate = fromDate
			}
		}

		if toDateStr := r.URL.Query().Get("to_date"); toDateStr != "" {
			if toDate, err := time.Parse(time.RFC3339, toDateStr); err == nil {
				filter.ToDate = toDate
			}
		}

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 100
		offset := 0

		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil {
				offset = o
			}
		}

		filter.Limit = limit
		filter.Offset = offset

		req := &database.GetChatHistoryRequest{
			SessionID: filter.SessionID,
			EventType: string(filter.EventType),
			FromDate:  filter.FromDate,
			ToDate:    filter.ToDate,
			Limit:     filter.Limit,
			Offset:    filter.Offset,
		}

		response, err := db.GetEvents(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// chatHistoryHealthCheckHandler health check for chat history
func chatHistoryHealthCheckHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"service": "chat-history",
		})
	}
}

// getAllSessionCostsHandler returns aggregate costs across all user's chat sessions
func getAllSessionCostsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		agentMode := r.URL.Query().Get("agent_mode")
		limitStr := r.URL.Query().Get("limit")

		limit := 100
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		var agentModePtr *string
		if agentMode != "" {
			agentModePtr = &agentMode
		}

		sessions, _, err := db.ListChatSessionsWithUser(r.Context(), limit, 0, nil, agentModePtr, userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to list sessions: %v", err), http.StatusInternalServerError)
			return
		}

		// Collect all session IDs for batch query
		sessionIDs := make([]string, len(sessions))
		sessionMap := make(map[string]*database.ChatHistorySummary, len(sessions))
		for i, s := range sessions {
			sessionIDs[i] = s.SessionID
			sCopy := s
			sessionMap[s.SessionID] = &sCopy
		}

		// Batch fetch token_usage + delegation_start events for ALL sessions in one query
		allEvents, err := batchGetCostEvents(r.Context(), db, sessionIDs)
		if err != nil {
			log.Printf("[COSTS] Error batch fetching cost events: %v", err)
			// Fall through with empty events
		}

		aggregate := AggregateCosts{
			ByModel: make(map[string]*ChatModelUsage),
			ByAgent: make(map[string]*ChatModelUsage),
		}

		var sessionCosts []SessionCostSummary

		for _, session := range sessions {
			sessEvents := allEvents[session.SessionID]

			// Build delegation name map from delegation_start events
			delegationNameMap := make(map[string]string)
			for _, ev := range sessEvents {
				if ev.eventType == "delegation_start" {
					delegationNameMap[ev.delegationID] = ev.bgAgentID
				}
			}

			// Determine display mode
			displayMode := session.AgentMode
			if session.AgentMode == "simple" {
				if cfg, err := database.ChatSessionConfigFromJSON(session.Config); err == nil && cfg != nil && cfg.DelegationMode == "plan" {
					displayMode = "multi-agent"
				}
			}

			summary := SessionCostSummary{
				SessionID: session.SessionID,
				Title:     session.Title,
				AgentMode: displayMode,
				CreatedAt: session.CreatedAt,
				Status:    session.Status,
				ByModel:   make(map[string]*ChatModelUsage),
				ByAgent:   make(map[string]*ChatModelUsage),
			}

			for _, ev := range sessEvents {
				if ev.eventType != "token_usage" {
					continue
				}
				tud := ev.tokenData

				// Resolve delegation component name
				if tud.correlationID != "" {
					if agentName, ok := delegationNameMap[tud.correlationID]; ok && agentName != "" {
						tud.Component = agentName
					}
				}

				modelUsage := getOrCreateModelUsage(summary.ByModel, tud.ModelID)
				accumulateUsage(modelUsage, &tud)

				if tud.Component != "" {
					agentUsage := getOrCreateModelUsage(summary.ByAgent, tud.Component)
					accumulateUsage(agentUsage, &tud)
				}

				summary.TotalCost += tud.TotalCost
				summary.TotalInput += tud.PromptTokens
				summary.TotalOutput += tud.CompletionTokens
				summary.TotalCalls++
			}

			sessionCosts = append(sessionCosts, summary)

			aggregate.TotalCost += summary.TotalCost
			aggregate.TotalInput += summary.TotalInput
			aggregate.TotalOutput += summary.TotalOutput
			aggregate.TotalCalls += summary.TotalCalls

			for modelID, usage := range summary.ByModel {
				aggModel := getOrCreateModelUsage(aggregate.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}
			for agentName, usage := range summary.ByAgent {
				aggAgent := getOrCreateModelUsage(aggregate.ByAgent, agentName)
				mergeUsage(aggAgent, usage)
			}
		}

		aggregate.TotalSessions = len(sessionCosts)

		if sessionCosts == nil {
			sessionCosts = []SessionCostSummary{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UserCostsResponse{
			Sessions:  sessionCosts,
			Aggregate: aggregate,
		})
	}
}

// getSessionCostsHandler returns detailed cost breakdown for a single session
func getSessionCostsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]
		if sessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}

		userID := GetUserIDFromContext(r.Context())

		session, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		// Use batch query for single session (same optimized path)
		allEvents, err := batchGetCostEvents(r.Context(), db, []string{sessionID})
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get token events: %v", err), http.StatusInternalServerError)
			return
		}
		sessEvents := allEvents[sessionID]

		// Build delegation name map
		delegationNameMap := make(map[string]string)
		for _, ev := range sessEvents {
			if ev.eventType == "delegation_start" {
				delegationNameMap[ev.delegationID] = ev.bgAgentID
			}
		}

		detail := SessionCostDetail{
			SessionID:       session.SessionID,
			Title:           session.Title,
			CreatedAt:       session.CreatedAt,
			ByModel:         make(map[string]*ChatModelUsage),
			ByTurnAndModel:  make(map[string]map[string]*ChatModelUsage),
			ByAgentAndModel: make(map[string]map[string]*ChatModelUsage),
		}

		for _, ev := range sessEvents {
			if ev.eventType != "token_usage" {
				continue
			}
			tud := ev.tokenData

			// Resolve delegation component name
			if tud.correlationID != "" {
				if agentName, ok := delegationNameMap[tud.correlationID]; ok && agentName != "" {
					tud.Component = agentName
				}
			}

			modelUsage := getOrCreateModelUsage(detail.ByModel, tud.ModelID)
			accumulateUsage(modelUsage, &tud)

			turnKey := fmt.Sprintf("turn-%d", tud.Turn)
			if detail.ByTurnAndModel[turnKey] == nil {
				detail.ByTurnAndModel[turnKey] = make(map[string]*ChatModelUsage)
			}
			turnModelUsage := getOrCreateModelUsage(detail.ByTurnAndModel[turnKey], tud.ModelID)
			accumulateUsage(turnModelUsage, &tud)

			agentKey := tud.Component
			if agentKey == "" {
				agentKey = "main"
			}
			if detail.ByAgentAndModel[agentKey] == nil {
				detail.ByAgentAndModel[agentKey] = make(map[string]*ChatModelUsage)
			}
			agentModelUsage := getOrCreateModelUsage(detail.ByAgentAndModel[agentKey], tud.ModelID)
			accumulateUsage(agentModelUsage, &tud)

			detail.TotalCost += tud.TotalCost
			detail.TotalInput += tud.PromptTokens
			detail.TotalOutput += tud.CompletionTokens
			detail.TotalCalls++
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(detail)
	}
}

// getDelegationLogsHandler returns delegation log entries with costs for a session (mux handler)
func getDelegationLogsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]
		if sessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}

		// Check session exists
		userID := GetUserIDFromContext(r.Context())
		_, err := db.GetChatSessionWithUser(r.Context(), sessionID, userID)
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		// Get all events for this session
		dbEvents, err := db.GetEventsBySession(r.Context(), sessionID, 10000, 0)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to get events: %v", err), http.StatusInternalServerError)
			return
		}

		// Also fetch sub-agent session events
		sqlDB := db.GetDB()
		var subSessionEvents []database.Event
		if sqlDB != nil {
			subSessionPrefix := sessionID + "-sub-"
			query := `SELECT session_id FROM chat_sessions WHERE session_id LIKE ?`
			if isPostgresDB(db) {
				query = `SELECT session_id FROM chat_sessions WHERE session_id LIKE $1`
			}
			rows, err := sqlDB.QueryContext(r.Context(),
				query,
				subSessionPrefix+"%",
			)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var subSessionID string
					if err := rows.Scan(&subSessionID); err != nil {
						continue
					}
					subEvents, err := db.GetEventsBySession(r.Context(), subSessionID, 10000, 0)
					if err == nil {
						subSessionEvents = append(subSessionEvents, subEvents...)
					}
				}
			}
		}

		allEvents := append(dbEvents, subSessionEvents...)

		// Build delegation entries from start/end events and aggregate token_usage
		delegationMap := make(map[string]*DelegationLogEntry)
		var delegationOrder []string

		for _, dbEvent := range allEvents {
			var wrapper struct {
				CorrelationID string          `json:"correlation_id"`
				Timestamp     time.Time       `json:"timestamp"`
				Data          json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(dbEvent.EventData, &wrapper); err != nil {
				continue
			}

			switch dbEvent.EventType {
			case "delegation_start":
				var startData struct {
					DelegationID      string   `json:"delegation_id"`
					Depth             int      `json:"depth"`
					Instruction       string   `json:"instruction"`
					ReasoningLevel    string   `json:"reasoning_level"`
					ModelID           string   `json:"model_id"`
					ToolMode          string   `json:"tool_mode"`
					Servers           []string `json:"servers"`
					BackgroundAgentID string   `json:"background_agent_id"`
					Timestamp         string   `json:"timestamp"`
				}
				if err := json.Unmarshal(wrapper.Data, &startData); err != nil {
					continue
				}

				entry := &DelegationLogEntry{
					DelegationID:      startData.DelegationID,
					Instruction:       startData.Instruction,
					ReasoningLevel:    startData.ReasoningLevel,
					ModelID:           startData.ModelID,
					ToolMode:          startData.ToolMode,
					Servers:           startData.Servers,
					BackgroundAgentID: startData.BackgroundAgentID,
					Depth:             startData.Depth,
					Status:            "running",
					StartTime:         startData.Timestamp,
					TokenUsage:        make(map[string]*ChatModelUsage),
				}
				delegationMap[startData.DelegationID] = entry
				delegationOrder = append(delegationOrder, startData.DelegationID)

			case "delegation_end":
				var endData struct {
					DelegationID string `json:"delegation_id"`
					Result       string `json:"result"`
					Error        string `json:"error"`
					Success      bool   `json:"success"`
					Timestamp    string `json:"timestamp"`
					InputTokens  int64  `json:"input_tokens"`
					OutputTokens int64  `json:"output_tokens"`
					ToolCalls    int64  `json:"tool_calls"`
					Duration     string `json:"duration"`
				}
				if err := json.Unmarshal(wrapper.Data, &endData); err != nil {
					continue
				}

				entry, ok := delegationMap[endData.DelegationID]
				if !ok {
					continue
				}

				if endData.Success {
					entry.Status = "completed"
				} else {
					entry.Status = "failed"
				}
				entry.EndTime = endData.Timestamp
				entry.Duration = endData.Duration
				entry.Result = endData.Result
				entry.Error = endData.Error
				entry.InputTokens = endData.InputTokens
				entry.OutputTokens = endData.OutputTokens
				entry.ToolCalls = endData.ToolCalls

			case "token_usage":
				correlationID := wrapper.CorrelationID
				if correlationID == "" {
					continue
				}

				entry, ok := delegationMap[correlationID]
				if !ok {
					continue
				}

				tud, err := parseTokenUsageEvent(dbEvent.EventData)
				if err != nil {
					continue
				}

				modelUsage := getOrCreateModelUsage(entry.TokenUsage, tud.ModelID)
				accumulateUsage(modelUsage, &tud)
				entry.TotalCostUSD += tud.TotalCost
			}
		}

		// Build ordered response
		response := DelegationLogsResponse{
			Delegations: make([]DelegationLogEntry, 0, len(delegationOrder)),
			ByModel:     make(map[string]*ChatModelUsage),
		}

		for _, id := range delegationOrder {
			entry := delegationMap[id]
			response.Delegations = append(response.Delegations, *entry)
			response.TotalCost += entry.TotalCostUSD
			response.TotalInput += entry.InputTokens
			response.TotalOutput += entry.OutputTokens
			response.TotalCalls++

			for modelID, usage := range entry.TokenUsage {
				aggModel := getOrCreateModelUsage(response.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getAllDelegationLogsHandler returns delegation logs grouped by session with main agent + sub-agent breakdown
func getAllDelegationLogsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserIDFromContext(r.Context())
		limitStr := r.URL.Query().Get("limit")

		limit := 50
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		sessions, _, err := db.ListChatSessionsWithUser(r.Context(), limit, 0, nil, nil, userID)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to list sessions: %v", err), http.StatusInternalServerError)
			return
		}

		// Build session info map and filter to multi-agent sessions
		type sessionInfo struct {
			summary database.ChatHistorySummary
		}
		var multiAgentSessions []sessionInfo
		for _, s := range sessions {
			cfg, err := database.ChatSessionConfigFromJSON(s.Config)
			if err == nil && cfg != nil && cfg.DelegationMode == "plan" {
				multiAgentSessions = append(multiAgentSessions, sessionInfo{summary: s})
			}
		}

		response := AllDelegationLogsResponse{
			Sessions: make([]SessionDelegationLogs, 0),
			ByModel:  make(map[string]*ChatModelUsage),
		}

		for _, si := range multiAgentSessions {
			sid := si.summary.SessionID
			dbEvents, err := db.GetEventsBySession(r.Context(), sid, 10000, 0)
			if err != nil {
				continue
			}

			sessLog := SessionDelegationLogs{
				SessionID:   sid,
				Title:       si.summary.Title,
				CreatedAt:   si.summary.CreatedAt,
				Status:      si.summary.Status,
				Delegations: make([]DelegationLogEntry, 0),
				ByModel:     make(map[string]*ChatModelUsage),
				MainAgent: AgentCostSummary{
					Name:    "Main Agent",
					ByModel: make(map[string]*ChatModelUsage),
				},
			}

			delegationMap := make(map[string]*DelegationLogEntry)
			var delegationOrder []string
			// Track which correlation_ids belong to delegations
			delegationCorrelations := make(map[string]bool)

			for _, dbEvent := range dbEvents {
				var wrapper struct {
					CorrelationID string          `json:"correlation_id"`
					Data          json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(dbEvent.EventData, &wrapper); err != nil {
					continue
				}

				switch dbEvent.EventType {
				case "delegation_start":
					var startData struct {
						DelegationID      string   `json:"delegation_id"`
						Depth             int      `json:"depth"`
						Instruction       string   `json:"instruction"`
						ReasoningLevel    string   `json:"reasoning_level"`
						ModelID           string   `json:"model_id"`
						ToolMode          string   `json:"tool_mode"`
						Servers           []string `json:"servers"`
						BackgroundAgentID string   `json:"background_agent_id"`
						Timestamp         string   `json:"timestamp"`
					}
					if err := json.Unmarshal(wrapper.Data, &startData); err != nil {
						continue
					}
					entry := &DelegationLogEntry{
						DelegationID:      startData.DelegationID,
						SessionID:         sid,
						Instruction:       startData.Instruction,
						ReasoningLevel:    startData.ReasoningLevel,
						ModelID:           startData.ModelID,
						ToolMode:          startData.ToolMode,
						Servers:           startData.Servers,
						BackgroundAgentID: startData.BackgroundAgentID,
						Depth:             startData.Depth,
						Status:            "running",
						StartTime:         startData.Timestamp,
						TokenUsage:        make(map[string]*ChatModelUsage),
					}
					delegationMap[startData.DelegationID] = entry
					delegationOrder = append(delegationOrder, startData.DelegationID)
					delegationCorrelations[startData.DelegationID] = true

				case "delegation_end":
					var endData struct {
						DelegationID string `json:"delegation_id"`
						Result       string `json:"result"`
						Error        string `json:"error"`
						Success      bool   `json:"success"`
						Timestamp    string `json:"timestamp"`
						InputTokens  int64  `json:"input_tokens"`
						OutputTokens int64  `json:"output_tokens"`
						ToolCalls    int64  `json:"tool_calls"`
						Duration     string `json:"duration"`
					}
					if err := json.Unmarshal(wrapper.Data, &endData); err != nil {
						continue
					}
					entry, ok := delegationMap[endData.DelegationID]
					if !ok {
						continue
					}
					if endData.Success {
						entry.Status = "completed"
					} else {
						entry.Status = "failed"
					}
					entry.EndTime = endData.Timestamp
					entry.Duration = endData.Duration
					entry.Result = endData.Result
					entry.Error = endData.Error
					entry.InputTokens = endData.InputTokens
					entry.OutputTokens = endData.OutputTokens
					entry.ToolCalls = endData.ToolCalls

				case "token_usage":
					tud, err := parseTokenUsageEvent(dbEvent.EventData)
					if err != nil {
						continue
					}

					correlationID := wrapper.CorrelationID
					if correlationID != "" {
						if entry, ok := delegationMap[correlationID]; ok {
							// Sub-agent token usage
							modelUsage := getOrCreateModelUsage(entry.TokenUsage, tud.ModelID)
							accumulateUsage(modelUsage, &tud)
							entry.TotalCostUSD += tud.TotalCost
							continue
						}
					}

					// Main agent token usage (no correlation_id or doesn't match a delegation)
					// NOTE: Main agent emits CUMULATIVE token_usage events (one per user turn).
					// Each event contains the running total, so we REPLACE to avoid double-counting.
					mainModel := getOrCreateModelUsage(sessLog.MainAgent.ByModel, tud.ModelID)
					replaceUsage(mainModel, &tud)
					sessLog.MainAgent.InputTokens = int64(tud.PromptTokens)
					sessLog.MainAgent.OutputTokens = int64(tud.CompletionTokens)
					sessLog.MainAgent.TotalCostUSD = tud.TotalCost
					sessLog.MainAgent.LLMCalls++
				}
			}

			// Build session-level aggregates
			for _, id := range delegationOrder {
				entry := delegationMap[id]
				sessLog.Delegations = append(sessLog.Delegations, *entry)
				for modelID, usage := range entry.TokenUsage {
					aggModel := getOrCreateModelUsage(sessLog.ByModel, modelID)
					mergeUsage(aggModel, usage)
				}
			}

			// Add main agent costs to session totals
			for modelID, usage := range sessLog.MainAgent.ByModel {
				aggModel := getOrCreateModelUsage(sessLog.ByModel, modelID)
				mergeUsage(aggModel, usage)
			}

			// Compute session totals
			for _, usage := range sessLog.ByModel {
				sessLog.TotalCost += usage.TotalCost
				sessLog.TotalInput += int64(usage.InputTokens)
				sessLog.TotalOutput += int64(usage.OutputTokens)
				sessLog.TotalCalls += int64(usage.LLMCallCount)
			}

			// Only include sessions that have delegations or main agent costs
			if len(sessLog.Delegations) > 0 || sessLog.MainAgent.LLMCalls > 0 {
				response.Sessions = append(response.Sessions, sessLog)
				response.TotalCost += sessLog.TotalCost
				response.TotalInput += sessLog.TotalInput
				response.TotalOutput += sessLog.TotalOutput
				response.TotalCalls += sessLog.TotalCalls
				for modelID, usage := range sessLog.ByModel {
					aggModel := getOrCreateModelUsage(response.ByModel, modelID)
					mergeUsage(aggModel, usage)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// getDelegationEventsHandler returns events for a specific delegation (drill-down, mux handler)
func getDelegationEventsHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sessionID := vars["session_id"]
		delegationID := vars["delegation_id"]

		if sessionID == "" || delegationID == "" {
			http.Error(w, "session_id and delegation_id are required", http.StatusBadRequest)
			return
		}

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 500
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		offset := 0
		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}

		// Query events with matching correlation_id
		dbEvents, err := db.GetEventsByCorrelationID(r.Context(), sessionID, delegationID, limit, offset)
		if err != nil || len(dbEvents) == 0 {
			// Fallback: try sub-agent session
			subSessionID := sessionID + "-sub-" + delegationID
			dbEvents, err = db.GetEventsBySession(r.Context(), subSessionID, limit, offset)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to get delegation events: %v", err), http.StatusInternalServerError)
				return
			}
		}

		type rawEvent struct {
			ID        string          `json:"id"`
			Type      string          `json:"type"`
			Timestamp time.Time       `json:"timestamp"`
			SessionID string          `json:"session_id"`
			Data      json.RawMessage `json:"data"`
		}

		events := make([]rawEvent, 0, len(dbEvents))
		for _, dbEvent := range dbEvents {
			events = append(events, rawEvent{
				ID:        dbEvent.ID,
				Type:      dbEvent.EventType,
				Timestamp: dbEvent.Timestamp,
				SessionID: sessionID,
				Data:      dbEvent.EventData,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": events,
			"total":  len(events),
		})
	}
}

// costEvent is a lightweight struct for batch cost queries (holds either token_usage or delegation_start data)
type costEvent struct {
	eventType    string         // "token_usage" or "delegation_start"
	tokenData    tokenUsageData // populated for token_usage events
	delegationID string         // populated for delegation_start events
	bgAgentID    string         // populated for delegation_start events
}

// batchGetCostEvents fetches token_usage and delegation_start events for multiple sessions in a single query
func batchGetCostEvents(ctx context.Context, db database.Database, sessionIDs []string) (map[string][]costEvent, error) {
	result := make(map[string][]costEvent)
	if len(sessionIDs) == 0 {
		return result, nil
	}

	sqlDB := db.GetDB()
	if sqlDB == nil {
		return result, fmt.Errorf("no database connection")
	}

	// Resolve session UUIDs to internal IDs
	// Build placeholder string for IN clause
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, sid := range sessionIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = sid
	}
	inClause := "(" + strings.Join(placeholders, ",") + ")"

	// Step 1: Resolve session_id UUIDs -> internal hex IDs
	resolveQuery := `SELECT id, session_id FROM chat_sessions WHERE session_id IN ` + inClause
	rows, err := sqlDB.QueryContext(ctx, resolveQuery, args...)
	if err != nil {
		return result, fmt.Errorf("failed to resolve session IDs: %w", err)
	}

	internalToUUID := make(map[string]string) // internal hex ID -> UUID
	internalIDs := make([]string, 0, len(sessionIDs))
	for rows.Next() {
		var internalID, uuid string
		if err := rows.Scan(&internalID, &uuid); err != nil {
			continue
		}
		internalToUUID[internalID] = uuid
		internalIDs = append(internalIDs, internalID)
	}
	rows.Close()

	if len(internalIDs) == 0 {
		return result, nil
	}

	// Step 2: Batch query events filtered by event_type
	placeholders2 := make([]string, len(internalIDs))
	args2 := make([]interface{}, len(internalIDs)+2)
	args2[0] = "token_usage"
	args2[1] = "delegation_start"
	for i, id := range internalIDs {
		placeholders2[i] = fmt.Sprintf("$%d", i+3)
		args2[i+2] = id
	}
	inClause2 := "(" + strings.Join(placeholders2, ",") + ")"

	eventsQuery := `SELECT chat_session_id, event_type, event_data FROM events
		WHERE event_type IN ($1, $2) AND chat_session_id IN ` + inClause2 + `
		ORDER BY timestamp ASC`

	eventRows, err := sqlDB.QueryContext(ctx, eventsQuery, args2...)
	if err != nil {
		return result, fmt.Errorf("failed to batch query events: %w", err)
	}
	defer eventRows.Close()

	for eventRows.Next() {
		var chatSessionID, eventType, eventDataJSON string
		if err := eventRows.Scan(&chatSessionID, &eventType, &eventDataJSON); err != nil {
			continue
		}

		// Map internal ID back to UUID
		uuid, ok := internalToUUID[chatSessionID]
		if !ok {
			continue
		}

		eventData := json.RawMessage(eventDataJSON)

		if eventType == "token_usage" {
			tud, err := parseTokenUsageEvent(eventData)
			if err != nil {
				continue
			}
			result[uuid] = append(result[uuid], costEvent{
				eventType: "token_usage",
				tokenData: tud,
			})
		} else if eventType == "delegation_start" {
			var wrapper struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(eventData, &wrapper); err != nil {
				continue
			}
			var delegData struct {
				DelegationID      string `json:"delegation_id"`
				BackgroundAgentID string `json:"background_agent_id"`
			}
			if err := json.Unmarshal(wrapper.Data, &delegData); err != nil {
				continue
			}
			result[uuid] = append(result[uuid], costEvent{
				eventType:    "delegation_start",
				delegationID: delegData.DelegationID,
				bgAgentID:    delegData.BackgroundAgentID,
			})
		}
	}

	return result, nil
}

// --- ACTIVE SESSION MANAGEMENT ---

// trackActiveSession tracks a new active session
func (api *StreamingAPI) trackActiveSession(sessionID, agentMode, query, userID string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	api.activeSessions[sessionID] = &ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    agentMode,
		Status:       "running",
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
		Query:        query,
		UserID:       userID,
	}

	log.Printf("[ACTIVE_SESSION] Tracked active session: %s (mode: %s, user: %s)", sessionID, agentMode, userID)
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

// updateSessionStatus updates the status of an active session
func (api *StreamingAPI) updateSessionStatus(sessionID, status string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	if session, exists := api.activeSessions[sessionID]; exists {
		session.Status = status
		session.LastActivity = time.Now()
		log.Printf("[ACTIVE_SESSION] Updated session %s status to: %s", sessionID, status)
	} else {
		log.Printf("[ACTIVE_SESSION] Session %s not found in activeSessions, updating database only", sessionID)
	}

	// Always update the database, regardless of whether session is in activeSessions
	go func() {
		ctx := context.Background()
		var completedAt *time.Time
		if status == "completed" {
			now := time.Now()
			completedAt = &now
		}

		log.Printf("[ACTIVE_SESSION] Updating database for session %s status to: %s", sessionID, status)
		_, err := api.chatDB.UpdateChatSession(ctx, sessionID, &database.UpdateChatSessionRequest{
			Status:      status,
			CompletedAt: completedAt,
		})
		if err != nil {
			log.Printf("[ACTIVE_SESSION] Failed to update database for session %s: %v", sessionID, err)
		} else {
			log.Printf("[ACTIVE_SESSION] Successfully updated database for session %s status to: %s", sessionID, status)
		}

		// NOTE: Completed sessions are NOT removed from activeSessions map immediately.
		// They remain in the map so getAllActiveSessions can include them in the 30-minute
		// window for page refresh restoration. The cleanupInactiveSessions goroutine will
		// eventually remove old sessions.
	}()
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

	// Update in-memory status
	api.activeSessionsMux.Lock()
	if session, exists := api.activeSessions[sessionID]; exists {
		session.Status = "dismissed"
		session.LastActivity = time.Now()
	}
	api.activeSessionsMux.Unlock()

	// Also update DB so the dismiss survives backend restarts.
	// The DB fallback in handleGetActiveSessions checks status — "dismissed" won't match.
	if api.chatDB != nil {
		if _, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, &database.UpdateChatSessionRequest{
			Status: "dismissed",
		}); err != nil {
			log.Printf("[ACTIVE_SESSION] Failed to dismiss session %s in DB: %v", sessionID, err)
		}
	}

	log.Printf("[ACTIVE_SESSION] Dismissed session %s", sessionID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "dismissed",
		"session": sessionID,
	})
}

// planEventEmitter implements virtualtools.PlanEventEmitter to emit workspace_file_operation events
// when delegation plan files are saved, so the workspace sidebar highlights them.
type planEventEmitter struct {
	eventStore *events.EventStore
	sessionID  string
	chatDB     database.Database
}

func (e *planEventEmitter) EmitFileEvent(filepath string) {
	now := time.Now()
	eventData := unifiedevents.NewWorkspaceFileOperationEvent("update", filepath, virtualtools.PlanFileFolderPath, 0, "delegation")
	event := events.Event{
		ID:        fmt.Sprintf("%s_plan_file_%d", e.sessionID, now.UnixNano()),
		Type:      "workspace_file_operation",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      unifiedevents.WorkspaceFileOperation,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "delegation",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[DELEGATION PLAN] Failed to persist event to DB: %v", err)
		}
	}
	log.Printf("[DELEGATION PLAN] Emitted workspace_file_operation event for plan file: %s (session: %s)", filepath, e.sessionID)
}

// sessionEventEmitter implements virtualtools.SessionEventEmitter to emit
// blocking_human_feedback events for the confirm_plan_execution tool.
type sessionEventEmitter struct {
	eventStore *events.EventStore
	sessionID  string
	chatDB     database.Database
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
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[PLAN APPROVAL] Failed to persist event to DB: %v", err)
		}
	}
	log.Printf("[PLAN APPROVAL] Emitted blocking_human_feedback event for plan approval (request_id: %s, session: %s)", requestID, e.sessionID)
}

func (e *sessionEventEmitter) EmitPlanApproval(question, contextText, yesLabel string) {
	now := time.Now()
	eventData := &orchEvents.PlanApprovalEvent{
		BaseEventData: unifiedevents.BaseEventData{
			Timestamp: now,
		},
		Question: question,
		Context:  contextText,
		YesLabel: yesLabel,
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_plan_approval_%d", e.sessionID, now.UnixNano()),
		Type:      "plan_approval",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      orchEvents.PlanApproval,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "delegation",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[PLAN APPROVAL] Failed to persist plan_approval to DB: %v", err)
		}
	}
	log.Printf("[PLAN APPROVAL] Emitted plan_approval event (session: %s)", e.sessionID)
}

func (e *sessionEventEmitter) EmitBlockingHumanQuestions(requestID string, questions []map[string]string) {
	now := time.Now()
	// Convert questions to the event struct format
	var eventQuestions []orchEvents.BlockingHumanQuestionsQuestion
	for _, q := range questions {
		eventQuestions = append(eventQuestions, orchEvents.BlockingHumanQuestionsQuestion{
			ID:       q["id"],
			Question: q["question"],
		})
	}
	eventData := &orchEvents.BlockingHumanQuestionsEvent{
		BaseEventData: unifiedevents.BaseEventData{
			Timestamp: now,
		},
		RequestID: requestID,
		Questions: eventQuestions,
		SessionID: e.sessionID,
	}
	event := events.Event{
		ID:        fmt.Sprintf("%s_human_questions_%d", e.sessionID, now.UnixNano()),
		Type:      "blocking_human_questions",
		Timestamp: now,
		SessionID: e.sessionID,
		Data: &unifiedevents.AgentEvent{
			Type:      orchEvents.BlockingHumanQuestions,
			Timestamp: now,
			SessionID: e.sessionID,
			Component: "human",
			Data:      eventData,
		},
	}
	e.eventStore.AddEvent(e.sessionID, event)
	if e.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := e.chatDB.StoreEvent(context.Background(), e.sessionID, event.Data); err != nil {
			log.Printf("[HUMAN QUESTIONS] Failed to persist event to DB: %v", err)
		}
	}
	log.Printf("[HUMAN QUESTIONS] Emitted blocking_human_questions event (request_id: %s, session: %s)", requestID, e.sessionID)
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
		tierConfig := resolveDelegationTierConfig(parentReq.DelegationTierConfig)
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

	// Resolve tool_mode for sub-agent
	toolMode, _ := ctx.Value(virtualtools.ToolModeKey).(string)
	if toolMode == "" && loadedTemplate != nil && loadedTemplate.Frontmatter.DefaultToolMode != "" {
		toolMode = loadedTemplate.Frontmatter.DefaultToolMode
		log.Printf("[DELEGATION] Using template default tool_mode: %s", toolMode)
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

	// Auto-enable tool search mode for sub-agents with many MCP servers (>3)
	// This prevents overwhelming the LLM with too many tool definitions
	useToolSearch := toolMode == "tool_search"
	useCodeExec := toolMode == "code_execution"
	if !useToolSearch && !useCodeExec {
		realServerCount := 0
		for _, s := range serversList {
			if s != "all" && s != mcpclient.NoServers {
				realServerCount++
			}
		}
		if realServerCount > 3 {
			useToolSearch = true
			toolMode = "tool_search"
			log.Printf("[DELEGATION] Auto-enabling tool search mode for sub-agent — %d MCP servers (>3)", realServerCount)
		}
	}

	// Extract background agent ID if this delegation was spawned by a background agent
	backgroundAgentID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)

	// Emit delegation_start event (after model and server resolution so we can include all info)
	api.emitDelegationStartEvent(sessionID, delegationID, currentDepth, instruction, reasoningLevel, modelID, toolMode, serversList, backgroundAgentID, agentTemplateName)

	// Convert API keys from parent request to LLM format (respecting locked providers)
	var apiKeys *llm.ProviderAPIKeys = &llm.ProviderAPIKeys{}

	// 1. Start with keys from parent request
	if parentReq.LLMConfig != nil && parentReq.LLMConfig.APIKeys != nil {
		apiKeys.OpenRouter = parentReq.LLMConfig.APIKeys.OpenRouter
		apiKeys.OpenAI = parentReq.LLMConfig.APIKeys.OpenAI
		apiKeys.Anthropic = parentReq.LLMConfig.APIKeys.Anthropic
		apiKeys.Vertex = parentReq.LLMConfig.APIKeys.Vertex
		apiKeys.GeminiCLI = parentReq.LLMConfig.APIKeys.GeminiCLI
		apiKeys.MiniMax = parentReq.LLMConfig.APIKeys.MiniMax
		apiKeys.MiniMaxCodingPlan = parentReq.LLMConfig.APIKeys.MiniMaxCodingPlan
		if parentReq.LLMConfig.APIKeys.Bedrock != nil {
			apiKeys.Bedrock = &llm.BedrockConfig{Region: parentReq.LLMConfig.APIKeys.Bedrock.Region}
		}
		if parentReq.LLMConfig.APIKeys.Azure != nil {
			apiKeys.Azure = &llm.AzureAPIConfig{
				Endpoint:   parentReq.LLMConfig.APIKeys.Azure.Endpoint,
				APIKey:     parentReq.LLMConfig.APIKeys.Azure.APIKey,
				APIVersion: parentReq.LLMConfig.APIKeys.Azure.APIVersion,
				Region:     parentReq.LLMConfig.APIKeys.Azure.Region,
			}
		}
	}

	// 2. Override if global lock is on OR specific provider is locked
	envKeys := buildProviderAPIKeysFromEnv()
	globalLocked := isGlobalLLMConfigLocked()

	if globalLocked {
		// Force single provider if global lock is on
		provStr, modelStr := getPrimaryProviderAndModelFromDefaults()
		supported := getSupportedProviders()
		if len(supported) > 0 {
			allowed := make(map[string]bool)
			for _, p := range supported {
				allowed[p] = true
			}
			if !allowed[provStr] {
				provStr = supported[0]
				modelStr = llm.GetDefaultModel(llm.Provider(provStr))
			}
		}
		provider = llm.Provider(provStr)
		modelID = modelStr
		// Use all env keys for simplicity/security in global lock mode
		apiKeys = envKeys
	} else {
		// Partial locking: override keys for locked providers
		if isProviderLocked("openrouter") {
			apiKeys.OpenRouter = envKeys.OpenRouter
		}
		if isProviderLocked("openai") {
			apiKeys.OpenAI = envKeys.OpenAI
		}
		if isProviderLocked("anthropic") {
			apiKeys.Anthropic = envKeys.Anthropic
		}
		if isProviderLocked("vertex") {
			apiKeys.Vertex = envKeys.Vertex
		}
		if isProviderLocked("bedrock") {
			apiKeys.Bedrock = envKeys.Bedrock
		}
		if isProviderLocked("azure") {
			apiKeys.Azure = envKeys.Azure
		}
		if isProviderLocked("gemini-cli") {
			apiKeys.GeminiCLI = envKeys.GeminiCLI
		}
		if isProviderLocked("minimax") {
			apiKeys.MiniMax = envKeys.MiniMax
		}
		if isProviderLocked("minimax-coding-plan") {
			apiKeys.MiniMaxCodingPlan = envKeys.MiniMaxCodingPlan
		}
	}

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
		Name:       fmt.Sprintf("%s-sub-%d-%d", sessionID, currentDepth, time.Now().UnixNano()),
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
			if parentReq.MaxTurns > 0 {
				return parentReq.MaxTurns
			}
			return 100
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
		UseToolSearchMode:    useToolSearch,
		// Pre-discovered tools: all virtual/custom tools stay visible in tool search mode
		// Only MCP server tools require discovery via search_tools
		PreDiscoveredTools: func() []string {
			if !useToolSearch {
				return nil
			}
			preDiscovered := collectVirtualToolNames(
				virtualtools.CreateWorkspaceAdvancedTools(),
				virtualtools.CreateHumanTools(),
			)
			if parentReq.EnableBrowserAccess != nil && *parentReq.EnableBrowserAccess {
				preDiscovered = append(preDiscovered, collectVirtualToolNames(virtualtools.CreateWorkspaceBrowserTools())...)
			}
			return preDiscovered
		}(),
		APIKeys:   apiKeys,
		Fallbacks: tierFallbacks,
		SessionID: subAgentSessionID, // Reuse parent session's MCP connections via registry, unless browser isolation requested
		UserID:    subAgentUserID,    // Per-user OAuth token isolation
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

	// Add event observers to sub-agent so its events appear in the UI
	// Events from sub-agent will be tagged with Component field for identification
	if underlyingAgent := subAgent.GetUnderlyingAgent(); underlyingAgent != nil {
		// Create in-memory event observer for real-time updates
		// DelegationEventObserver tags events with correlation_id/parent_id and also persists to DB
		// (replaces the separate EventDatabaseObserver to avoid untagged duplicates)
		subAgentObserver := events.NewDelegationEventObserver(api.eventStore, sessionID, currentDepth, delegationID, api.logger)
		// Wire tool event callback if provided (background agents use this for timing)
		if toolCb, ok := ctx.Value(virtualtools.ToolEventCallbackKey).(events.ToolEventCallback); ok && toolCb != nil {
			subAgentObserver.OnToolEvent = toolCb
		}
		// Wire DB persistence so tagged sub-agent events are stored for shared sessions / restore
		subAgentObserver.DBStore = func(ctx context.Context, sid string, evt *unifiedevents.AgentEvent) error {
			return api.chatDB.StoreEvent(ctx, sid, evt)
		}
		underlyingAgent.AddEventListener(subAgentObserver)
		log.Printf("[DELEGATION] Added event observers for sub-agent at depth %d", currentDepth)

		// Add skill instructions to sub-agent system prompt (mirrors parent agent setup)
		if len(parentReq.SelectedSkills) > 0 {
			skillPrompt := buildSkillPrompt(parentReq.SelectedSkills, getWorkspaceAPIURL())
			if skillPrompt != "" {
				underlyingAgent.AppendSystemPrompt(skillPrompt)
				log.Printf("[DELEGATION] Added skill instructions to sub-agent (%d skills)", len(parentReq.SelectedSkills))
			}
		}

		// Add skill builder instructions for sub-agents when skill-creator is active
		hasSkillCreator := false
		for _, s := range parentReq.SelectedSkills {
			if s == "skill-creator" {
				hasSkillCreator = true
				break
			}
		}
		if hasSkillCreator {
			underlyingAgent.AppendSystemPrompt(GetSkillBuilderInstructions())
			log.Printf("[DELEGATION] Added skill builder instructions to sub-agent")
		}

		// Inject sub-agent template instructions into system prompt
		if loadedTemplate != nil {
			templatePrompt := fmt.Sprintf("\n## Sub-Agent Role: %s\n\n%s\n",
				loadedTemplate.Frontmatter.Name, loadedTemplate.Content)
			underlyingAgent.AppendSystemPrompt(templatePrompt)
			log.Printf("[DELEGATION] Injected sub-agent template instructions: %s", loadedTemplate.Frontmatter.Name)
		}

		// Merge global secrets with parent's decrypted secrets, then inject into sub-agent
		allDelegationSecrets := mergeGlobalSecrets(parentReq.DecryptedSecrets, parentReq.SelectedGlobalSecrets)
		if len(allDelegationSecrets) > 0 {
			var secretParts []string
			for _, s := range allDelegationSecrets {
				secretParts = append(secretParts, fmt.Sprintf("### %s\n```\n%s\n```", s.Name, s.Value))
			}
			secretPrompt := "\n## 🔐 Secrets\n\nThe following secrets/credentials have been provided. They are also available as environment variables in execute_shell_command with a SECRET_ prefix (e.g., os.environ[\"SECRET_MY_KEY\"] in Python or $SECRET_MY_KEY in bash).\n\n" + strings.Join(secretParts, "\n")
			underlyingAgent.AppendSystemPrompt(secretPrompt)
			log.Printf("[DELEGATION] Injected %d secrets (%d global + %d user) into sub-agent system prompt", len(allDelegationSecrets), len(allDelegationSecrets)-len(parentReq.DecryptedSecrets), len(parentReq.DecryptedSecrets))
		}

		// Give sub-agents the workspace folder structure so they know where to
		// read/write files (Chats/, Plans/, skills/, etc.).
		// The main agent skips this in plan mode (it's an orchestrator), but sub-agents
		// are actual file workers that need this orientation.
		underlyingAgent.AppendSystemPrompt(GetAgentInstructions())
		log.Printf("[DELEGATION] Added workspace folder instructions to sub-agent")

		// Give sub-agents access to memory tools so they can persist key discoveries
		// across tasks (reads from Plans/memories/ by default).
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
			log.Printf("[BROWSER] Added browser instructions to sub-agent (playwright=%v, camofox=%v, agent-browser=%v, cdp=%v)",
				subBrowserCfg.HasPlaywright, subBrowserCfg.HasCamofox, subBrowserCfg.HasAgentBrowser, subBrowserCfg.CdpPort > 0)
		}

		// [GWS] Add GWS quick-start instructions to sub-agent (same as parent)
		if parentReq.EnableGWSAccess != nil && *parentReq.EnableGWSAccess {
			underlyingAgent.AppendSystemPrompt(browserinstructions.GetGWSQuickStartInstructions())
			log.Printf("[GWS] Added GWS quick-start instructions to sub-agent")
		}

		// Register file path transformer for browser file uploads on sub-agent
		hasBrowserAccess := parentReq.EnableBrowserAccess != nil && *parentReq.EnableBrowserAccess
		hasPlaywright := false
		hasCamofox := false
		for _, s := range parentReq.EnabledServers {
			if s == "playwright" {
				hasPlaywright = true
			}
			if s == "camofox" {
				hasCamofox = true
			}
		}
		if hasBrowserAccess || hasPlaywright || hasCamofox {
			wsAbsPath, err := filepath.Abs("../workspace-docs/_users/default")
			if err == nil {
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
		}

		// Browser isolation: when share_browser=false, tell the sub-agent to use a unique
		// session name with the agent_browser tool to avoid sharing browser state.
		if sb, ok := ctx.Value(virtualtools.ShareBrowserKey).(bool); ok && !sb {
			underlyingAgent.AppendSystemPrompt(fmt.Sprintf("## Browser Isolation\nYou have an isolated browser session. When using the agent_browser tool, use a unique session name (e.g., \"isolated-%d\") instead of \"default\" to avoid sharing browser state with other agents.", time.Now().UnixNano()))
			log.Printf("[DELEGATION] Added browser isolation guidance to sub-agent system prompt")
		}
	}

	// Register the same workspace tools as parent (if workspace access is enabled)
	if underlyingAgent := subAgent.GetUnderlyingAgent(); underlyingAgent != nil {
		// Check workspace access from parent request
		enableWorkspaceAccess := true
		if parentReq.EnableWorkspaceAccess != nil {
			enableWorkspaceAccess = *parentReq.EnableWorkspaceAccess
		}
		if len(parentReq.SelectedSkills) > 0 {
			enableWorkspaceAccess = true
		}

		if enableWorkspaceAccess {
			// Sub-agents get advanced workspace tools (shell, image, web fetch, PDF, diff_patch)
			workspaceTools := virtualtools.CreateWorkspaceAdvancedTools()
			workspaceExecutors, subAgentEnv := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithSession(subAgentUserID, sessionID)
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
			_, _, toolCategories := createCustomTools(false)

			// Check for skill-creator
			hasSkillCreator := false
			for _, s := range parentReq.SelectedSkills {
				if s == "skill-creator" {
					hasSkillCreator = true
					break
				}
			}

			// Check for subagent-creator
			hasSubAgentCreator := false
			for _, s := range parentReq.SelectedSkills {
				if s == "subagent-creator" || s == "custom/subagent-creator" {
					hasSubAgentCreator = true
					break
				}
			}

			// Extract @context file paths from parent query for additional write access
			fileContextFolders := extractFileContextWriteFolders(parentReq.Query)
			if len(fileContextFolders) > 0 {
				log.Printf("[DELEGATION] Extracted write paths from parent @context: %v", fileContextFolders)
			}

			// Apply folder guards
			// Plan sub-agents: restrict to the plan folder
			// All others: default Chats/ guard
			planFolder, _ := ctx.Value(virtualtools.PlanFolderKey).(string)
			if planFolder != "" {
				// Tighter restriction: only allow writes to plan folder (e.g. Plans/{planID}/)
				additionalFolders := []string{}
				if hasSkillCreator {
					additionalFolders = append(additionalFolders, "skills/custom/")
				}
				if hasSubAgentCreator {
					additionalFolders = append(additionalFolders, "subagents/custom/")
				}
				additionalFolders = append(additionalFolders, fileContextFolders...)
				workspaceExecutors = wrapExecutorsWithPlanFolderGuard(workspaceExecutors, planFolder, additionalFolders...)
				workspace.SetSessionWorkingDir(sessionID, planFolder+"/")
				workspace.SetSessionFolderGuard(sessionID,
					append([]string{planFolder + "/", "skills/", "subagents/", "Downloads/"}, additionalFolders...),
					append([]string{planFolder + "/"}, additionalFolders...),
				)
				log.Printf("[DELEGATION] Applied plan folder guard: writes restricted to %s/", planFolder)
			} else {
				extraFolders := []string{}
				if hasSkillCreator {
					extraFolders = append(extraFolders, "skills/custom/")
				}
				if hasSubAgentCreator {
					extraFolders = append(extraFolders, "subagents/custom/")
				}
				extraFolders = append(extraFolders, fileContextFolders...)
				workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors, extraFolders...)
				workspace.SetSessionWorkingDir(sessionID, "Chats/")
				workspace.SetSessionFolderGuard(sessionID,
					append([]string{"Chats/", "Downloads/", "Plans/", "skills/", "subagents/", "Workflow/"}, extraFolders...),
					append([]string{"Chats/", "Downloads/"}, extraFolders...),
				)
			}

			// Register workspace tools
			for _, tool := range workspaceTools {
				if tool.Function == nil {
					continue
				}
				toolName := tool.Function.Name
				if executor, exists := workspaceExecutors[toolName]; exists {
					enhancedDescription := enhanceToolDescriptionForChatMode(toolName, tool.Function.Description)

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

				browserExtraFolders := []string{}
				if hasSkillCreator {
					browserExtraFolders = append(browserExtraFolders, "skills/custom/")
				}
				browserExtraFolders = append(browserExtraFolders, fileContextFolders...)
				browserExecutors = wrapExecutorsWithChatModeFolderGuard(browserExecutors, browserExtraFolders...)

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

			// Register image generation tool for sub-agent if enabled in parent request
			if parentReq.EnableImageGeneration != nil && *parentReq.EnableImageGeneration {
				imgCfg := virtualtools.ImageGenExecutorConfig{
					Provider:        "vertex",
					ModelID:         "gemini-2.5-flash-image",
					WorkspaceAPIURL: getWorkspaceAPIURL(),
					UserID:          subAgentUserID,
				}
				if parentReq.ImageGenConfig != nil {
					if parentReq.ImageGenConfig.Provider != "" {
						imgCfg.Provider = parentReq.ImageGenConfig.Provider
					}
					if parentReq.ImageGenConfig.ModelID != "" {
						imgCfg.ModelID = parentReq.ImageGenConfig.ModelID
					}
					imgCfg.APIKey = parentReq.ImageGenConfig.APIKey
				}
				for _, toolDef := range []struct {
					tool     func() llmtypes.Tool
					executor func(virtualtools.ImageGenExecutorConfig) func(context.Context, map[string]any) (string, error)
					category func() string
				}{
					{virtualtools.GetImageGenToolDefinition, virtualtools.CreateImageGenExecutor, virtualtools.GetImageGenToolCategory},
					{virtualtools.GetImageEditToolDefinition, virtualtools.CreateImageEditExecutor, virtualtools.GetImageEditToolCategory},
				} {
					t := toolDef.tool()
					exec := toolDef.executor(imgCfg)
					var params map[string]interface{}
					if t.Function.Parameters != nil {
						paramsBytes, err := json.Marshal(t.Function.Parameters)
						if err == nil {
							json.Unmarshal(paramsBytes, &params)
						}
					}
					if params != nil {
						if err := underlyingAgent.RegisterCustomTool(
							t.Function.Name,
							t.Function.Description,
							params,
							exec,
							toolDef.category(),
						); err != nil {
							log.Printf("[IMAGE GEN] Warning: Failed to register %s for sub-agent: %v", t.Function.Name, err)
						} else {
							log.Printf("[IMAGE GEN] Registered %s for sub-agent (provider=%s model=%s)", t.Function.Name, imgCfg.Provider, imgCfg.ModelID)
						}
					}
				}
			}

			// NOTE: Sub-agents do NOT get the delegate tool themselves (v1 design choice)
			// This prevents runaway delegation chains

			// Apply CLI provider override to sub-agents when the parent uses a CLI-based provider.
			// Without this, sub-agents spawned by a claude-code/gemini-cli main agent would try to
			// call workspace/delegation tools as native Bash/Read/Write — which are blocked.
			if parentReq.Provider == "claude-code" || parentReq.Provider == "gemini-cli" || parentReq.Provider == "codex-cli" {
				underlyingAgent.AppendSystemPrompt(virtualtools.GetClaudeCodeDelegationOverride())
				log.Printf("[DELEGATION] Applied CLI provider override to sub-agent (provider: %s)", parentReq.Provider)
			}

			// Add plan update instructions to worker sub-agents
			// Workers update plan.md as they work — adding findings, marking progress, noting issues
			if planFolder != "" {
				planUpdatePrompt := fmt.Sprintf(`
## Workspace Rules
Your workspace folder is: %s/
Save ALL output files inside this folder. Do NOT write to Chats/ or any other folder.

**File Organization — ALWAYS use sub-folders:**
- Organize outputs into sub-folders by type — NEVER dump files at the plan folder root
- Use descriptive sub-folder names: research/, reports/, scripts/, data/, config/, analysis/, etc.
- Match the sub-folder to your task type. Examples:
  - Research/analysis outputs → %s/research/topic-name.md
  - Generated reports → %s/reports/report-name.md
  - Scripts or code → %s/scripts/script-name.py
  - Data files → %s/data/dataset-name.json
- If your instruction specifies a path, use that exact path
- Create the sub-folder if it doesn't exist (mkdir -p via execute_shell_command)
- Only plan.md and plan_tracking.md belong at the folder root

## Plan Update Protocol
You are working on a task from the plan at %s/plan.md.
After completing your task, you MUST update the plan file:
1. **Mark your task**: Change '[ ]' to '[x]' for your completed task using sed
2. **Add key knowledge**: Append important discoveries to the '## Key Knowledge' section — things that other workers (who start fresh with NO context) need to know. Examples: file paths created/modified, API endpoints discovered, configuration values, naming conventions found, gotchas or constraints discovered.
3. **Add results**: Append a brief summary of what you did to the '## Notes' section
4. **Report issues**: If something failed or was unexpected, note it in '## Notes'

This is critical — the manager reads plan.md after you finish and passes your Key Knowledge to the next worker. Without your updates, the next worker will have no context about what you discovered.

Use execute_shell_command or diff_patch_workspace_file to update the file.
- For appending: execute_shell_command(command: "echo '\n- [task-N result]: Summary of findings' >> %s/plan.md")
- For precise edits (marking checkboxes, updating sections): diff_patch_workspace_file with filepath "%s/plan.md"
`, planFolder, planFolder, planFolder, planFolder, planFolder, planFolder, planFolder, planFolder)
				underlyingAgent.AppendSystemPrompt(planUpdatePrompt)
				log.Printf("[DELEGATION] Added plan update instructions for plan folder: %s", planFolder)
			} else {
				// No plan folder — ad-hoc delegate. Give the sub-agent a minimal worker context
				// so it knows to return a clear result rather than asking follow-up questions.
				underlyingAgent.AppendSystemPrompt(`## Your Role
You are a focused background worker. Complete the assigned task using available tools and return a clear, concise result.
- You cannot spawn further sub-agents
- You have no shared memory with the caller — all context is in the instruction you received
- Save any output files to Chats/ unless the instruction specifies otherwise
- Return a summary of what you did and what you found`)
				log.Printf("[DELEGATION] Added ad-hoc worker context to sub-agent (no plan folder)")
			}
		}
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

// todoSubAgentBgNotifier implements step_based_workflow.SubAgentNotifier using bgAgentRegistry.
// It registers todo task sub-agents so the main workshop agent receives a synthetic turn
// (auto-notification) when each completes.
type todoSubAgentBgNotifier struct {
	api       *StreamingAPI
	sessionID string
}

func (n *todoSubAgentBgNotifier) OnSubAgentStart(agentID, name string) {
	bgAgent := &BackgroundAgent{
		ID:        agentID,
		Name:      name,
		SessionID: n.sessionID,
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
	}
	n.api.bgAgentRegistry.Register(n.sessionID, bgAgent)

	// Pre-create the channel synchronously so NotifyCompletion never drops
	// a completion due to a race with backgroundCompletionLoop startup.
	n.api.bgAgentRegistry.GetNotificationChannel(n.sessionID)

	// Ensure the background completion loop is running for this session so that
	// NotifyCompletion's channel send is actually consumed.
	n.api.completionLoopStartedMu.Lock()
	if !n.api.completionLoopStarted[n.sessionID] {
		n.api.completionLoopStarted[n.sessionID] = true
		go n.api.backgroundCompletionLoop(n.sessionID)
	}
	n.api.completionLoopStartedMu.Unlock()
}

func (n *todoSubAgentBgNotifier) OnSubAgentComplete(agentID, name, result string, err error) {
	agent := n.api.bgAgentRegistry.Get(n.sessionID, agentID)
	if agent == nil {
		return
	}
	if err != nil {
		agent.SetError(err.Error())
	} else {
		agent.SetResult(result)
	}
	n.api.bgAgentRegistry.NotifyCompletion(n.sessionID, agentID)
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

	// Copy only the context values actually needed by executeDelegatedTask.
	// Note: DelegationDepthKey is NOT copied because background sub-agents don't have
	// the delegate tool, so they can never create further sub-agents.
	if rl, ok := ctx.Value(virtualtools.ReasoningLevelKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.ReasoningLevelKey, rl)
	}
	if pf, ok := ctx.Value(virtualtools.PlanFolderKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.PlanFolderKey, pf)
	}
	if tm, ok := ctx.Value(virtualtools.ToolModeKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.ToolModeKey, tm)
	}
	if at, ok := ctx.Value(virtualtools.AgentTemplateKey).(string); ok {
		bgCtx = context.WithValue(bgCtx, virtualtools.AgentTemplateKey, at)
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
		ID:          agentID,
		Name:        name,
		SessionID:   sessionID,
		Instruction: instruction,
		Status:      BGAgentRunning,
		CreatedAt:   time.Now(),
		cancel:      bgCancel,
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
	// Also persist to database so shared/restored sessions include background agent events
	if api.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := api.chatDB.StoreEvent(context.Background(), sessionID, event.Data); err != nil {
			log.Printf("[BG_AGENT] Failed to persist %s event to DB: %v", eventType, err)
		}
	}
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

	for agentID := range ch {
		if api.isSessionBusy(sessionID) {
			// Session is busy — queue the completion for later processing
			api.queuePendingCompletion(sessionID, agentID)
			log.Printf("[BG AGENT] Session %s busy, queued completion for agent %s", sessionID, agentID)
		} else {
			api.processBackgroundAgentCompletion(sessionID, agentID)
		}
	}

	log.Printf("[BG AGENT] Completion loop ended for session %s", sessionID)
}

// processBatchedBackgroundAgentCompletions builds a single [AUTO-NOTIFICATION] message for one or more
// completed agents and fires ONE synthetic turn. Subsequent drained completions are chained via
// the synthetic turn's own defer, avoiding concurrent StreamWithEvents calls.
func (api *StreamingAPI) processBatchedBackgroundAgentCompletions(sessionID string, agentIDs []string) {
	if len(agentIDs) == 0 {
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
		var resultText string
		if snap.Status == BGAgentCompleted {
			resultText = truncateForToolResponse(snap.Result, 1000)
		} else if snap.Status == BGAgentFailed {
			resultText = fmt.Sprintf("Error: %s", snap.Error)
		} else {
			resultText = fmt.Sprintf("Status: %s", snap.Status)
		}
		parts = append(parts, fmt.Sprintf("- **%s** (ID: %s): %s\n  Result: %s", snap.Name, snap.ID, snap.Status, resultText))
		emittedIDs = append(emittedIDs, agentID)
	}

	if len(parts) == 0 {
		return
	}

	syntheticMsg := fmt.Sprintf("[AUTO-NOTIFICATION] Multiple step completions:\n%s", strings.Join(parts, "\n"))

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

// processBackgroundAgentCompletion injects a synthetic message and triggers a new main agent turn
func (api *StreamingAPI) processBackgroundAgentCompletion(sessionID, agentID string) {
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

	var resultText string
	if snap.Status == BGAgentCompleted {
		resultText = truncateForToolResponse(snap.Result, 4000)
	} else if snap.Status == BGAgentFailed {
		resultText = fmt.Sprintf("Error: %s", snap.Error)
	} else {
		resultText = fmt.Sprintf("Status: %s", snap.Status)
	}

	syntheticMsg := fmt.Sprintf(
		"[AUTO-NOTIFICATION]\nAgent '%s' (ID: %s) completed.\nStatus: %s\nResult:\n%s",
		snap.Name, snap.ID, snap.Status, resultText)

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

// executeSyntheticTurn drives the stored agent directly with a synthetic message.
// Instead of creating an internal HTTP request and re-building the entire agent/tools/history,
// it reuses the agent stored after the last plan-mode turn via StreamWithEvents().
// This is called synchronously from processBackgroundAgentCompletion — it sets session busy
// before spawning the goroutine, preventing concurrent synthetic turns.
func (api *StreamingAPI) executeSyntheticTurn(sessionID, syntheticMsg string) {
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

	// Inject user ID into context for per-user folder isolation
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

			// Clear session busy and drain pending completions (batched to avoid concurrent StreamWithEvents)
			api.setSessionBusy(sessionID, false)
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

		// Final save of conversation history
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = llmAgent.GetHistory()
		api.conversationMux.Unlock()
		log.Printf("[BG AGENT] Synthetic turn completed for session %s, history: %d messages", sessionID, len(llmAgent.GetHistory()))

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
	for _, s := range req.EnabledServers {
		if s == "camofox" {
			hasBrowser = true
			break
		}
	}
	caps := &virtualtools.CapabilitiesContext{
		EnabledServers: req.EnabledServers,
		SelectedTools:  req.SelectedTools,
		HasWorkspace:   req.EnableWorkspaceAccess == nil || *req.EnableWorkspaceAccess,
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
			DefaultToolMode:       sa.Frontmatter.DefaultToolMode,
		})
	}

	return caps
}

// emitDelegationStartEvent emits an event when delegation starts
// This event serves as the parent for all sub-agent events (via parent_id linking)
func (api *StreamingAPI) emitDelegationStartEvent(sessionID, delegationID string, depth int, instruction, reasoningLevel, modelID, toolMode string, servers []string, backgroundAgentID, agentTemplate string) {
	now := time.Now()
	eventID := fmt.Sprintf("%s_delegation_start_%s", sessionID, delegationID)
	eventData := &events.DelegationStartEventData{
		DelegationID:      delegationID,
		Depth:             depth,
		Instruction:       instruction,
		ReasoningLevel:    reasoningLevel,
		ModelID:           modelID,
		ToolMode:          toolMode,
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
	// Also persist to database so shared sessions and restored sessions include delegation events
	if api.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := api.chatDB.StoreEvent(context.Background(), sessionID, event.Data); err != nil {
			log.Printf("[DELEGATION] Failed to persist delegation_start to DB: %v", err)
		}
	}
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
	// Also persist to database so shared sessions and restored sessions include delegation events
	if api.chatDB != nil && eventbridge.ShouldStoreEvent(string(event.Data.Type)) {
		if err := api.chatDB.StoreEvent(context.Background(), sessionID, event.Data); err != nil {
			log.Printf("[DELEGATION] Failed to persist delegation_end to DB: %v", err)
		}
	}
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
			if (session.Status == "completed" || session.Status == "inactive") && now.Sub(session.LastActivity) >= completedSessionRetention {
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
	enabledGroupIDs []string,
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
		displayName := g.DisplayName
		if displayName == "" {
			displayName = g.GroupID
		}
		// Mark the user-selected group
		selected := ""
		for _, eid := range enabledGroupIDs {
			if eid == g.GroupID {
				selected = " **[SELECTED]**"
				break
			}
		}
		sb.WriteString(fmt.Sprintf("- **%s** (group_id: `%s`, %s)%s\n", displayName, g.GroupID, status, selected))
	}

	if selectedRunFolder != "" {
		sb.WriteString(fmt.Sprintf("\nSelected run folder: `%s`\n", selectedRunFolder))
	}

	if len(enabledGroupIDs) > 0 {
		sb.WriteString(fmt.Sprintf("\nUser has selected group(s): %v — use these as default for execute_step calls.\n", enabledGroupIDs))
	}

	return sb.String()
}

// resolveLatestRunFolder lists the runs/ directory and returns the name of the latest
// iteration folder (e.g. "iteration-27"). Returns "" if no runs exist or on error.
func resolveLatestRunFolder(ctx context.Context, workspacePath string, wsClient *workspace.Client) string {
	runsPath := workspacePath + "/runs"
	maxDepth := 1
	resp, err := wsClient.ListWorkspaceFiles(ctx, workspace.ListWorkspaceFilesParams{
		Folder:   runsPath,
		MaxDepth: &maxDepth,
	})
	if err != nil {
		return ""
	}

	// Parse response — try the three known formats
	type wsFile struct {
		FilePath string `json:"filepath"`
		Type     string `json:"type"`
	}
	var files []wsFile
	if err := json.Unmarshal([]byte(resp), &files); err != nil {
		var wrapped struct {
			Files []wsFile `json:"files"`
		}
		if err2 := json.Unmarshal([]byte(resp), &wrapped); err2 == nil {
			files = wrapped.Files
		} else {
			var api struct {
				Data []wsFile `json:"data"`
			}
			if err3 := json.Unmarshal([]byte(resp), &api); err3 == nil {
				files = api.Data
			}
		}
	}

	maxIter := 0
	for _, f := range files {
		if f.Type != "folder" {
			continue
		}
		name := f.FilePath
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		var n int
		if _, err := fmt.Sscanf(name, "iteration-%d", &n); err == nil && n > maxIter {
			maxIter = n
		}
	}
	if maxIter == 0 {
		return ""
	}
	return fmt.Sprintf("iteration-%d", maxIter)
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
) *todo_creation_human.WorkshopConfig {
	// Extract enabled group IDs from execution options (toolbar selection)
	var enabledGroupIDs []string
	if req.ExecutionOptions != nil && len(req.ExecutionOptions.EnabledGroupIDs) > 0 {
		enabledGroupIDs = req.ExecutionOptions.EnabledGroupIDs
	}

	cfg := &todo_creation_human.WorkshopConfig{
		WorkspacePath:     workspacePath,
		RunFolder:         runFolder,
		MCPConfigPath:     api.mcpConfigPath,
		SelectedServers:   append([]string(nil), selectedServers...), // copy to avoid mutation
		LLMConfig:         req.LLMConfig,
		UseKnowledgebase:  true,
		LLMAllocationMode: "manual",
		Logger:            api.logger,
		SessionID:         sessionID,
		EnabledGroupIDs:   enabledGroupIDs,
	}

	// Build base tools: workspace_advanced + workspace_basic + human + todo
	// Same as createCustomTools(true) in tool_setup.go
	allTools, allExecutors, toolCategories := createCustomTools(true)

	// Track preset's global secret selection (overrides req.SelectedGlobalSecrets which is nil for phase chat)
	var presetGlobalSecretNames *[]string

	// Load full preset config (same logic as normal workflow handler)
	if req.PresetQueryID != "" {
		wsCtx, wsCancel := context.WithTimeout(context.Background(), 10*time.Second)
		preset, wsErr := api.chatDB.GetPresetQuery(wsCtx, req.PresetQueryID)
		wsCancel()
		if wsErr != nil {
			log.Printf("[WORKSHOP] Warning: Failed to load preset %s: %v", req.PresetQueryID, wsErr)
		} else if preset != nil {
			log.Printf("[WORKSHOP] Loaded preset %s for full config", req.PresetQueryID)

			// Selected tools
			if preset.SelectedTools != "" {
				if err := json.Unmarshal([]byte(preset.SelectedTools), &cfg.SelectedTools); err != nil {
					log.Printf("[WORKSHOP] Failed to parse selected tools: %v", err)
				}
			}

			// Code execution mode
			cfg.UseCodeExecutionMode = preset.UseCodeExecutionMode
			cfg.UseToolSearchMode = preset.UseToolSearchMode

			// Selected skills
			if preset.SelectedSkills != "" {
				var skills []string
				if err := json.Unmarshal([]byte(preset.SelectedSkills), &skills); err != nil {
					log.Printf("[WORKSHOP] Failed to parse selected skills: %v", err)
				} else if len(skills) > 0 {
					cfg.SelectedSkills = skills
					log.Printf("[WORKSHOP] Loaded %d skills from preset: %v", len(skills), skills)
				}
			}

			// Global secret selection from preset (overrides nil req.SelectedGlobalSecrets for phase chat)
			// DB stores NULL (Go empty string), "[]" = none, "[...]" = specific
			// NULL (never configured) defaults to NO secrets — global secrets must be explicitly selected.
			if preset.SelectedGlobalSecretNames != "" {
				var names []string
				if err := json.Unmarshal([]byte(preset.SelectedGlobalSecretNames), &names); err != nil {
					log.Printf("[WORKSHOP] Failed to parse selected_global_secret_names: %v", err)
				} else {
					presetGlobalSecretNames = &names
					log.Printf("[WORKSHOP] Loaded %d selected global secret names from preset", len(names))
				}
			} else {
				// Preset never configured global secrets — default to none, not all.
				// Global secrets should only be injected when explicitly selected.
				emptyNames := []string{}
				presetGlobalSecretNames = &emptyNames
				log.Printf("[WORKSHOP] No global secrets configured in preset — defaulting to none")
			}

			// Pre-discovered tools
			if preset.PreDiscoveredTools != "" {
				if err := json.Unmarshal([]byte(preset.PreDiscoveredTools), &cfg.PreDiscoveredTools); err != nil {
					log.Printf("[WORKSHOP] Failed to parse pre-discovered tools: %v", err)
				}
			}

			// Browser tools: resolve effective browser mode from preset
			effectiveBrowserMode := preset.BrowserMode
			if effectiveBrowserMode == "" && preset.EnableBrowserAccess {
				effectiveBrowserMode = "headless" // Legacy preset without browser_mode
			}
			// agent-browser tool is used for headless and CDP modes (not playwright/camofox which use MCP servers)
			if effectiveBrowserMode == "headless" || effectiveBrowserMode == "cdp" {
				// Resolve CDP port: prefer request, fall back to preset browser_mode, default 9222 for CDP
				cdpPortForBrowser := getCdpPort(req)
				if cdpPortForBrowser == 0 && effectiveBrowserMode == "cdp" {
					cdpPortForBrowser = 9222 // Default CDP port when preset says CDP but request doesn't include port
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

				// Strip playwright/camofox MCP servers (headless/CDP mode uses agent_browser)
				var filteredServers []string
				for _, s := range cfg.SelectedServers {
					if s != "playwright" && s != "camofox" {
						filteredServers = append(filteredServers, s)
					}
				}
				if len(filteredServers) != len(cfg.SelectedServers) {
					log.Printf("[WORKSHOP] Stripped playwright/camofox from server list (was %d, now %d)", len(cfg.SelectedServers), len(filteredServers))
					cfg.SelectedServers = filteredServers
				}
			}

			// LLM config from preset
			if len(preset.LLMConfig) > 0 {
				var presetLLMConfig database.PresetLLMConfig
				if jsonErr := json.Unmarshal(preset.LLMConfig, &presetLLMConfig); jsonErr != nil {
					log.Printf("[WORKSHOP] Failed to parse preset LLM config: %v", jsonErr)
				} else {
					// Extract preset phase LLM
					cfg.PresetPhaseLLM = workshopExtractLLM(presetLLMConfig.PhaseLLM, presetLLMConfig.Provider, presetLLMConfig.ModelID)
					log.Printf("[WORKSHOP] LLM config: phase=%v", cfg.PresetPhaseLLM != nil)

					// Knowledgebase toggle
					if presetLLMConfig.UseKnowledgebase != nil {
						cfg.UseKnowledgebase = *presetLLMConfig.UseKnowledgebase
					}

					// Tiered LLM allocation
					if presetLLMConfig.LLMAllocationMode == "tiered" && presetLLMConfig.TieredConfig != nil {
						cfg.LLMAllocationMode = "tiered"
						cfg.TieredConfig = &todo_creation_human.TieredLLMConfig{
							Tier1: &todo_creation_human.AgentLLMConfig{
								Provider:  presetLLMConfig.TieredConfig.Tier1.Provider,
								ModelID:   presetLLMConfig.TieredConfig.Tier1.ModelID,
								Fallbacks: workshopConvertFallbacks(presetLLMConfig.TieredConfig.Tier1.Fallbacks),
							},
							Tier2: &todo_creation_human.AgentLLMConfig{
								Provider:  presetLLMConfig.TieredConfig.Tier2.Provider,
								ModelID:   presetLLMConfig.TieredConfig.Tier2.ModelID,
								Fallbacks: workshopConvertFallbacks(presetLLMConfig.TieredConfig.Tier2.Fallbacks),
							},
							Tier3: &todo_creation_human.AgentLLMConfig{
								Provider:  presetLLMConfig.TieredConfig.Tier3.Provider,
								ModelID:   presetLLMConfig.TieredConfig.Tier3.ModelID,
								Fallbacks: workshopConvertFallbacks(presetLLMConfig.TieredConfig.Tier3.Fallbacks),
							},
						}
						log.Printf("[WORKSHOP] Tiered mode: T1=%s/%s T2=%s/%s T3=%s/%s",
							cfg.TieredConfig.Tier1.Provider, cfg.TieredConfig.Tier1.ModelID,
							cfg.TieredConfig.Tier2.Provider, cfg.TieredConfig.Tier2.ModelID,
							cfg.TieredConfig.Tier3.Provider, cfg.TieredConfig.Tier3.ModelID)
					}

					// Image generation tools
					if presetLLMConfig.EnableImageGeneration != nil && *presetLLMConfig.EnableImageGeneration {
						imgCfg := virtualtools.ImageGenExecutorConfig{
							Provider:        "vertex",
							ModelID:         "gemini-2.5-flash-image",
							WorkspaceAPIURL: getWorkspaceAPIURL(),
							UserID:          currentUserID,
						}
						if presetLLMConfig.ImageGenProvider != "" {
							imgCfg.Provider = presetLLMConfig.ImageGenProvider
						}
						if presetLLMConfig.ImageGenModelID != "" {
							imgCfg.ModelID = presetLLMConfig.ImageGenModelID
						}
						for _, def := range []struct {
							tool     func() llmtypes.Tool
							executor func(virtualtools.ImageGenExecutorConfig) func(context.Context, map[string]any) (string, error)
							category func() string
						}{
							{virtualtools.GetImageGenToolDefinition, virtualtools.CreateImageGenExecutor, virtualtools.GetImageGenToolCategory},
							{virtualtools.GetImageEditToolDefinition, virtualtools.CreateImageEditExecutor, virtualtools.GetImageEditToolCategory},
						} {
							t := def.tool()
							exec := def.executor(imgCfg)
							allTools = append(allTools, t)
							allExecutors[t.Function.Name] = exec
							toolCategories[t.Function.Name] = def.category()
							log.Printf("[WORKSHOP] Registered image tool: %s (provider=%s model=%s)", t.Function.Name, imgCfg.Provider, imgCfg.ModelID)
						}
					}
				}
			}
		}
	}

	// Merge secrets — use preset's global secret selection if available (phase chat doesn't send req.SelectedGlobalSecrets)
	effectiveGlobalSecretSelection := req.SelectedGlobalSecrets
	if presetGlobalSecretNames != nil {
		effectiveGlobalSecretSelection = presetGlobalSecretNames
	}
	allSecrets := mergeGlobalSecrets(req.DecryptedSecrets, effectiveGlobalSecretSelection)
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
	log.Printf("[WORKSHOP] Replaced workspace executors with session-aware versions (sessionID=%q, secrets=%d)", sessionID, len(secretEnvVars))

	cfg.CustomTools = allTools
	cfg.CustomToolExecutors = allExecutors
	cfg.ToolCategories = toolCategories

	// Create workshop event bridge for SSE emission from background goroutines
	cfg.EventBridge = &eventbridge.WorkflowEventBridge{
		BaseEventBridge: &eventbridge.BaseEventBridge{
			EventStore: api.eventStore,
			SessionID:  sessionID,
			Logger:     api.logger,
			ChatDB:     nil,
			BridgeName: "workshop",
		},
	}

	// Wire up live tool call query for query_step_tools
	cfg.ToolCallQueryFunc = formatToolCallSummaries(api)

	// Wire up schedule management callbacks
	cfg.PresetQueryID = req.PresetQueryID
	cfg.SchedulerFuncs = api.buildSchedulerCallbacks()
	cfg.SkillFuncs = api.buildSkillCallbacks()
	cfg.ListAvailableSecrets = func(ctx context.Context) ([]string, error) {
		nameSet := make(map[string]bool)
		// Global secrets from env vars
		for _, gs := range getGlobalSecrets() {
			nameSet[gs.Name] = true
		}
		// User-stored secrets from DB
		userSecrets, err := api.chatDB.ListUserSecrets(ctx, currentUserID)
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

	return cfg
}

// buildSchedulerCallbacks creates SchedulerCallbacks that bridge the workshop tools
// to the database and scheduler service. Returns nil-safe callbacks.
func (api *StreamingAPI) buildSchedulerCallbacks() *todo_creation_human.SchedulerCallbacks {
	return &todo_creation_human.SchedulerCallbacks{
		ListSchedules: func(ctx context.Context, presetID string) (string, error) {
			jobs, total, err := api.chatDB.ListScheduledJobs(ctx, 50, 0, nil, nil)
			if err != nil {
				return "", err
			}
			// Filter by presetID
			var filtered []database.ScheduledJob
			for _, j := range jobs {
				if j.PresetQueryID == presetID {
					filtered = append(filtered, j)
				}
			}
			if len(filtered) == 0 {
				return fmt.Sprintf("No schedules found for this workflow (total schedules across all workflows: %d).", total), nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Schedules (%d found)\n\n", len(filtered)))
			for _, j := range filtered {
				status := "disabled"
				if j.Enabled {
					status = "enabled"
				}
				sb.WriteString(fmt.Sprintf("### %s\n", j.Name))
				sb.WriteString(fmt.Sprintf("- **ID**: `%s`\n", j.ID))
				sb.WriteString(fmt.Sprintf("- **Cron**: `%s`\n", j.CronExpression))
				sb.WriteString(fmt.Sprintf("- **Timezone**: %s\n", j.Timezone))
				sb.WriteString(fmt.Sprintf("- **Status**: %s\n", status))
				if j.LastStatus != "" {
					sb.WriteString(fmt.Sprintf("- **Last Run**: %v (status: %s)\n", j.LastRunAt, j.LastStatus))
				}
				if j.NextRunAt != nil {
					sb.WriteString(fmt.Sprintf("- **Next Run**: %v\n", j.NextRunAt))
				}
				if len(j.GroupIDs) > 0 {
					sb.WriteString(fmt.Sprintf("- **Groups**: %v\n", j.GroupIDs))
				} else {
					sb.WriteString("- **Groups**: all\n")
				}
				sb.WriteString(fmt.Sprintf("- **Run Count**: %d\n\n", j.RunCount))
			}
			return sb.String(), nil
		},
		CreateSchedule: func(ctx context.Context, presetID, name, cronExpr, timezone string, groupIDs []string) (string, error) {
			if err := ValidateCronExpression(cronExpr); err != nil {
				return "", fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
			}
			if timezone == "" {
				timezone = "UTC"
			}
			enabled := true
			job, err := api.chatDB.CreateScheduledJob(ctx, &database.CreateScheduledJobRequest{
				Name:           name,
				EntityType:     "workflow",
				PresetQueryID:  presetID,
				CronExpression: cronExpr,
				Timezone:       timezone,
				GroupIDs:       groupIDs,
				Enabled:        &enabled,
			})
			if err != nil {
				return "", err
			}
			// Load into gocron scheduler
			if api.scheduler != nil {
				if err := api.scheduler.LoadJob(job); err != nil {
					return fmt.Sprintf("Schedule created (ID: %s) but failed to activate: %v", job.ID, err), nil
				}
			}
			nextRun := ""
			if job.NextRunAt != nil {
				nextRun = fmt.Sprintf("%v", job.NextRunAt)
			}
			return fmt.Sprintf("Schedule created and activated.\n- **ID**: `%s`\n- **Name**: %s\n- **Cron**: `%s`\n- **Timezone**: %s\n- **Next Run**: %s", job.ID, job.Name, job.CronExpression, job.Timezone, nextRun), nil
		},
		UpdateSchedule: func(ctx context.Context, jobID, name, cronExpr, timezone string, groupIDs []string, setGroupIDs bool, enabled *bool) (string, error) {
			if cronExpr != "" {
				if err := ValidateCronExpression(cronExpr); err != nil {
					return "", fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
				}
			}
			req := &database.UpdateScheduledJobRequest{
				Name:           name,
				CronExpression: cronExpr,
				Timezone:       timezone,
				GroupIDs:       groupIDs,
				SetGroupIDs:    setGroupIDs,
				Enabled:        enabled,
			}
			job, err := api.chatDB.UpdateScheduledJob(ctx, jobID, req)
			if err != nil {
				return "", err
			}
			// Reload in gocron
			if api.scheduler != nil {
				if job.Enabled {
					if err := api.scheduler.LoadJob(job); err != nil {
						return fmt.Sprintf("Schedule updated but failed to reload: %v", err), nil
					}
				} else {
					api.scheduler.RemoveJob(jobID)
				}
			}
			nextRun := ""
			if job.NextRunAt != nil {
				nextRun = fmt.Sprintf("%v", job.NextRunAt)
			}
			return fmt.Sprintf("Schedule updated.\n- **ID**: `%s`\n- **Name**: %s\n- **Cron**: `%s`\n- **Enabled**: %v\n- **Next Run**: %s", job.ID, job.Name, job.CronExpression, job.Enabled, nextRun), nil
		},
		DeleteSchedule: func(ctx context.Context, jobID string) error {
			if api.scheduler != nil {
				api.scheduler.RemoveJob(jobID)
			}
			return api.chatDB.DeleteScheduledJob(ctx, jobID)
		},
		TriggerSchedule: func(ctx context.Context, jobID string) (string, error) {
			if api.scheduler == nil {
				return "", fmt.Errorf("scheduler not available")
			}
			sessionID, err := api.scheduler.TriggerNow(jobID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Schedule triggered. Execution session: `%s`", sessionID), nil
		},
		GetScheduleRuns: func(ctx context.Context, jobID string, limit int) (string, error) {
			if limit <= 0 {
				limit = 10
			}
			runs, total, err := api.chatDB.ListScheduledJobRuns(ctx, jobID, limit, 0)
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
				sb.WriteString(fmt.Sprintf("- **%s** [%s]%s — %s", r.ID[:8], r.Status, duration, r.StartedAt.Format("2006-01-02 15:04:05")))
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
	return &todo_creation_human.SkillCallbacks{
		ListSkills: func(ctx context.Context) (string, error) {
			workspaceAPIURL := api.GetAPIURL()
			allSkills, err := skills.DiscoverSkills(workspaceAPIURL)
			if err != nil {
				return "", fmt.Errorf("failed to discover skills: %w", err)
			}
			if len(allSkills) == 0 {
				return "No skills found in the workspace. Use import_skill to add skills from GitHub.", nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("## Skills (%d found)\n\n", len(allSkills)))
			for _, sk := range allSkills {
				sb.WriteString(fmt.Sprintf("### %s\n", sk.Frontmatter.Name))
				sb.WriteString(fmt.Sprintf("- **Folder**: `%s`\n", sk.FolderName))
				if sk.Frontmatter.Description != "" {
					sb.WriteString(fmt.Sprintf("- **Description**: %s\n", sk.Frontmatter.Description))
				}
				sb.WriteString("\n")
			}
			return sb.String(), nil
		},
		ImportSkill: func(ctx context.Context, githubURL, token string) (string, error) {
			workspaceAPIURL := api.GetAPIURL()
			resp, err := skills.ImportGitHubSkill(workspaceAPIURL, githubURL, token)
			if err != nil {
				return "", fmt.Errorf("failed to import skill: %w", err)
			}
			if !resp.Success {
				return fmt.Sprintf("Failed to import skill: %s", resp.Error), nil
			}
			return fmt.Sprintf("Successfully imported skill **%s**. Use update_workflow_config to add it to the workflow's selected skills.", resp.SkillName), nil
		},
		DeleteSkill: func(ctx context.Context, folderName string) error {
			workspaceAPIURL := api.GetAPIURL()
			return skills.DeleteSkill(workspaceAPIURL, folderName)
		},
	}
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

func getOrganizationChatInstructions() string {
	return `# Organization Assistant

You manage organization operations across workflows.

Primary responsibilities:
1. Manage employees.
2. Assign multiple workflows to employees.
3. Manage workflow schedules.
4. Inspect latest workflow outputs and recent scheduled runs.

Operating rules:
- Stay in organization scope unless the user explicitly asks to switch to workflow design.
- Prefer the dedicated organization tools over ad-hoc shell/database inspection.
- When you change an employee assignment or schedule, state exactly what changed.
- Treat "output" as the latest workflow run and its artifacts unless the user defines a stricter business-output contract.
- If a workflow has no runs yet, say that explicitly instead of implying an output exists.`
}

func marshalOrganizationResult(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (api *StreamingAPI) registerOrganizationChatTools(underlyingAgent *mcpagent.Agent, currentUserID string, sessionID string) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	wsClient := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithUserID(currentUserID),
	)

	listWorkflowSummaries := func(ctx context.Context) ([]map[string]interface{}, error) {
		var (
			presets []database.PresetQuery
			err     error
		)
		if currentUserID != "" {
			presets, _, err = api.chatDB.ListPresetQueriesWithUser(ctx, 500, 0, currentUserID)
		} else {
			presets, _, err = api.chatDB.ListPresetQueries(ctx, 500, 0)
		}
		if err != nil {
			return nil, err
		}

		employees, err := api.chatDB.ListEmployees(ctx)
		if err != nil {
			return nil, err
		}
		employeeNames := make(map[string]string, len(employees))
		for _, emp := range employees {
			employeeNames[emp.ID] = emp.Name
		}

		entityType := database.ScheduleEntityWorkflow
		jobs, _, err := api.chatDB.ListScheduledJobs(ctx, 500, 0, &entityType, nil)
		if err != nil {
			return nil, err
		}
		scheduleCountByPreset := make(map[string]int, len(jobs))
		for _, job := range jobs {
			scheduleCountByPreset[job.PresetQueryID]++
		}

		workflows := make([]map[string]interface{}, 0, len(presets))
		for _, preset := range presets {
			if preset.AgentMode != database.AgentModeWorkflow {
				continue
			}

			summary := map[string]interface{}{
				"preset_query_id":    preset.ID,
				"label":              preset.Label,
				"employee_id":        nil,
				"employee_name":      nil,
				"workspace_path":     "",
				"schedule_count":     scheduleCountByPreset[preset.ID],
				"latest_run_folder":  nil,
				"latest_output_path": nil,
				"latest_logs_path":   nil,
			}

			if preset.EmployeeID.Valid && preset.EmployeeID.String != "" {
				summary["employee_id"] = preset.EmployeeID.String
				if name := employeeNames[preset.EmployeeID.String]; name != "" {
					summary["employee_name"] = name
				}
			}

			if preset.SelectedFolder.Valid && preset.SelectedFolder.String != "" {
				workspacePath := preset.SelectedFolder.String
				summary["workspace_path"] = workspacePath
				latestRunFolder := resolveLatestRunFolder(ctx, workspacePath, wsClient)
				if latestRunFolder != "" {
					summary["latest_run_folder"] = latestRunFolder
					summary["latest_output_path"] = filepath.ToSlash(filepath.Join(workspacePath, "runs", latestRunFolder, "execution"))
					summary["latest_logs_path"] = filepath.ToSlash(filepath.Join(workspacePath, "runs", latestRunFolder, "logs"))
				}
			}

			workflows = append(workflows, summary)
		}

		sort.Slice(workflows, func(i, j int) bool {
			li, _ := workflows[i]["label"].(string)
			lj, _ := workflows[j]["label"].(string)
			return strings.ToLower(li) < strings.ToLower(lj)
		})

		return workflows, nil
	}

	registerTool := func(name, description string, params map[string]interface{}, exec func(context.Context, map[string]interface{}) (string, error)) error {
		return underlyingAgent.RegisterCustomTool(name, description, params, exec, "organization_tools")
	}

	if err := registerTool("list_employees", "List all employees available for workflow assignment.", map[string]interface{}{}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		employees, err := api.chatDB.ListEmployees(ctx)
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"employees": employees})
	}); err != nil {
		return err
	}

	if err := registerTool("create_employee", "Create a new employee record.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":         map[string]interface{}{"type": "string", "description": "Employee name."},
			"avatar_color": map[string]interface{}{"type": "string", "description": "Optional hex color.", "default": "#6366f1"},
			"description":  map[string]interface{}{"type": "string", "description": "Optional employee description or role."},
		},
		"required": []string{"name"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		name, _ := args["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		avatarColor, _ := args["avatar_color"].(string)
		description, _ := args["description"].(string)
		created, err := api.chatDB.CreateEmployee(ctx, &database.Employee{
			Name:        name,
			AvatarColor: strings.TrimSpace(avatarColor),
			Description: strings.TrimSpace(description),
			UserID:      currentUserID,
		})
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"employee": created, "success": true})
	}); err != nil {
		return err
	}

	if err := registerTool("update_employee", "Update an existing employee record.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"employee_id":  map[string]interface{}{"type": "string", "description": "Employee ID."},
			"name":         map[string]interface{}{"type": "string", "description": "Updated employee name."},
			"avatar_color": map[string]interface{}{"type": "string", "description": "Updated hex color."},
			"description":  map[string]interface{}{"type": "string", "description": "Updated description or role."},
		},
		"required": []string{"employee_id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		employeeID, _ := args["employee_id"].(string)
		employeeID = strings.TrimSpace(employeeID)
		if employeeID == "" {
			return "", fmt.Errorf("employee_id is required")
		}
		current, err := api.chatDB.GetEmployee(ctx, employeeID)
		if err != nil {
			return "", err
		}
		if current == nil {
			return "", fmt.Errorf("employee %q not found", employeeID)
		}
		if name, ok := args["name"].(string); ok && strings.TrimSpace(name) != "" {
			current.Name = strings.TrimSpace(name)
		}
		if avatarColor, ok := args["avatar_color"].(string); ok && strings.TrimSpace(avatarColor) != "" {
			current.AvatarColor = strings.TrimSpace(avatarColor)
		}
		if description, ok := args["description"].(string); ok {
			current.Description = strings.TrimSpace(description)
		}
		updated, err := api.chatDB.UpdateEmployee(ctx, employeeID, current)
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"employee": updated, "success": true})
	}); err != nil {
		return err
	}

	if err := registerTool("delete_employee", "Delete an employee. Assigned workflows become unassigned.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"employee_id": map[string]interface{}{"type": "string", "description": "Employee ID."},
		},
		"required": []string{"employee_id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		employeeID, _ := args["employee_id"].(string)
		employeeID = strings.TrimSpace(employeeID)
		if employeeID == "" {
			return "", fmt.Errorf("employee_id is required")
		}
		if err := api.chatDB.DeleteEmployee(ctx, employeeID); err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"success": true, "employee_id": employeeID})
	}); err != nil {
		return err
	}

	if err := registerTool("list_workflows", "List workflow presets with current employee assignment, schedules, and latest output locations.", map[string]interface{}{}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		workflows, err := listWorkflowSummaries(ctx)
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"workflows": workflows})
	}); err != nil {
		return err
	}

	if err := registerTool("assign_workflow_employee", "Assign or unassign a workflow preset to an employee.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"preset_query_id": map[string]interface{}{"type": "string", "description": "Workflow preset ID."},
			"employee_id":     map[string]interface{}{"type": "string", "description": "Employee ID. Leave empty to unassign."},
		},
		"required": []string{"preset_query_id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		presetQueryID, _ := args["preset_query_id"].(string)
		presetQueryID = strings.TrimSpace(presetQueryID)
		if presetQueryID == "" {
			return "", fmt.Errorf("preset_query_id is required")
		}
		employeeID, _ := args["employee_id"].(string)
		employeeID = strings.TrimSpace(employeeID)

		sqlDB := api.chatDB.GetDB()
		if sqlDB == nil {
			return "", fmt.Errorf("database connection unavailable")
		}

		var (
			result interface{ RowsAffected() (int64, error) }
			err    error
		)
		if employeeID != "" {
			result, err = sqlDB.ExecContext(ctx,
				`UPDATE preset_queries SET employee_id = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
				employeeID, presetQueryID)
		} else {
			result, err = sqlDB.ExecContext(ctx,
				`UPDATE preset_queries SET employee_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1`,
				presetQueryID)
		}
		if err != nil {
			return "", err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return "", err
		}
		if rowsAffected == 0 {
			return "", fmt.Errorf("preset_query_id %q not found", presetQueryID)
		}

		return marshalOrganizationResult(map[string]interface{}{
			"success":         true,
			"preset_query_id": presetQueryID,
			"employee_id":     employeeID,
		})
	}); err != nil {
		return err
	}

	if err := registerTool("list_workflow_schedules", "List schedules for workflow presets. Optionally filter by preset_query_id.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"preset_query_id": map[string]interface{}{"type": "string", "description": "Optional workflow preset ID filter."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		presetQueryID, _ := args["preset_query_id"].(string)
		presetQueryID = strings.TrimSpace(presetQueryID)
		entityType := database.ScheduleEntityWorkflow
		jobs, _, err := api.chatDB.ListScheduledJobs(ctx, 500, 0, &entityType, nil)
		if err != nil {
			return "", err
		}
		filtered := make([]database.ScheduledJob, 0, len(jobs))
		for _, job := range jobs {
			if presetQueryID != "" && job.PresetQueryID != presetQueryID {
				continue
			}
			filtered = append(filtered, job)
		}
		return marshalOrganizationResult(map[string]interface{}{"jobs": filtered})
	}); err != nil {
		return err
	}

	if err := registerTool("create_workflow_schedule", "Create a schedule for a workflow preset.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"preset_query_id": map[string]interface{}{"type": "string", "description": "Workflow preset ID."},
			"name":            map[string]interface{}{"type": "string", "description": "Schedule name."},
			"cron_expression": map[string]interface{}{"type": "string", "description": "Cron expression."},
			"timezone":        map[string]interface{}{"type": "string", "description": "Timezone name.", "default": "Asia/Kolkata"},
			"description":     map[string]interface{}{"type": "string", "description": "Optional description."},
			"enabled":         map[string]interface{}{"type": "boolean", "description": "Whether schedule is enabled.", "default": true},
			"group_ids":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional workflow group IDs."},
		},
		"required": []string{"preset_query_id", "name", "cron_expression"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		presetQueryID, _ := args["preset_query_id"].(string)
		name, _ := args["name"].(string)
		cronExpression, _ := args["cron_expression"].(string)
		timezone, _ := args["timezone"].(string)
		description, _ := args["description"].(string)
		if strings.TrimSpace(presetQueryID) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(cronExpression) == "" {
			return "", fmt.Errorf("preset_query_id, name, and cron_expression are required")
		}
		if strings.TrimSpace(timezone) == "" {
			timezone = "Asia/Kolkata"
		}
		var groupIDs []string
		if rawGroupIDs, ok := args["group_ids"].([]interface{}); ok {
			for _, item := range rawGroupIDs {
				if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
					groupIDs = append(groupIDs, strings.TrimSpace(value))
				}
			}
		}
		enabled := true
		if value, ok := args["enabled"].(bool); ok {
			enabled = value
		}
		job, err := api.chatDB.CreateScheduledJob(ctx, &database.CreateScheduledJobRequest{
			Name:           strings.TrimSpace(name),
			Description:    strings.TrimSpace(description),
			EntityType:     database.ScheduleEntityWorkflow,
			PresetQueryID:  strings.TrimSpace(presetQueryID),
			GroupIDs:       groupIDs,
			CronExpression: strings.TrimSpace(cronExpression),
			Timezone:       strings.TrimSpace(timezone),
			Enabled:        &enabled,
		})
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"job": job, "success": true})
	}); err != nil {
		return err
	}

	if err := registerTool("update_workflow_schedule", "Update an existing workflow schedule.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"schedule_id":     map[string]interface{}{"type": "string", "description": "Schedule ID."},
			"name":            map[string]interface{}{"type": "string", "description": "Updated name."},
			"description":     map[string]interface{}{"type": "string", "description": "Updated description."},
			"cron_expression": map[string]interface{}{"type": "string", "description": "Updated cron expression."},
			"timezone":        map[string]interface{}{"type": "string", "description": "Updated timezone."},
			"enabled":         map[string]interface{}{"type": "boolean", "description": "Enable or disable the schedule."},
			"group_ids":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Updated workflow group IDs."},
		},
		"required": []string{"schedule_id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		scheduleID, _ := args["schedule_id"].(string)
		scheduleID = strings.TrimSpace(scheduleID)
		if scheduleID == "" {
			return "", fmt.Errorf("schedule_id is required")
		}
		req := &database.UpdateScheduledJobRequest{}
		if name, ok := args["name"].(string); ok && strings.TrimSpace(name) != "" {
			req.Name = strings.TrimSpace(name)
		}
		if description, ok := args["description"].(string); ok {
			req.Description = strings.TrimSpace(description)
		}
		if cronExpression, ok := args["cron_expression"].(string); ok && strings.TrimSpace(cronExpression) != "" {
			req.CronExpression = strings.TrimSpace(cronExpression)
		}
		if timezone, ok := args["timezone"].(string); ok && strings.TrimSpace(timezone) != "" {
			req.Timezone = strings.TrimSpace(timezone)
		}
		if enabled, ok := args["enabled"].(bool); ok {
			req.Enabled = &enabled
		}
		if rawGroupIDs, ok := args["group_ids"].([]interface{}); ok {
			req.SetGroupIDs = true
			for _, item := range rawGroupIDs {
				if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
					req.GroupIDs = append(req.GroupIDs, strings.TrimSpace(value))
				}
			}
		}
		job, err := api.chatDB.UpdateScheduledJob(ctx, scheduleID, req)
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"job": job, "success": true})
	}); err != nil {
		return err
	}

	if err := registerTool("delete_workflow_schedule", "Delete a workflow schedule.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"schedule_id": map[string]interface{}{"type": "string", "description": "Schedule ID."},
		},
		"required": []string{"schedule_id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		scheduleID, _ := args["schedule_id"].(string)
		scheduleID = strings.TrimSpace(scheduleID)
		if scheduleID == "" {
			return "", fmt.Errorf("schedule_id is required")
		}
		if err := api.chatDB.DeleteScheduledJob(ctx, scheduleID); err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"success": true, "schedule_id": scheduleID})
	}); err != nil {
		return err
	}

	if err := registerTool("list_schedule_runs", "List recent runs for a schedule.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"schedule_id": map[string]interface{}{"type": "string", "description": "Schedule ID."},
			"limit":       map[string]interface{}{"type": "number", "description": "Maximum runs to return.", "default": 20},
		},
		"required": []string{"schedule_id"},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		scheduleID, _ := args["schedule_id"].(string)
		scheduleID = strings.TrimSpace(scheduleID)
		if scheduleID == "" {
			return "", fmt.Errorf("schedule_id is required")
		}
		limit := 20
		if rawLimit, ok := args["limit"].(float64); ok && rawLimit > 0 {
			limit = int(rawLimit)
		}
		runs, _, err := api.chatDB.ListScheduledJobRuns(ctx, scheduleID, limit, 0)
		if err != nil {
			return "", err
		}
		return marshalOrganizationResult(map[string]interface{}{"runs": runs})
	}); err != nil {
		return err
	}

	if err := registerTool("inspect_workflow_outputs", "Show the latest run/output paths for workflow presets. Optionally filter by preset_query_id.", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"preset_query_id": map[string]interface{}{"type": "string", "description": "Optional workflow preset ID filter."},
		},
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		presetQueryID, _ := args["preset_query_id"].(string)
		presetQueryID = strings.TrimSpace(presetQueryID)
		workflows, err := listWorkflowSummaries(ctx)
		if err != nil {
			return "", err
		}
		if presetQueryID != "" {
			filtered := make([]map[string]interface{}, 0, 1)
			for _, workflow := range workflows {
				if workflowID, _ := workflow["preset_query_id"].(string); workflowID == presetQueryID {
					filtered = append(filtered, workflow)
				}
			}
			workflows = filtered
		}
		return marshalOrganizationResult(map[string]interface{}{"outputs": workflows})
	}); err != nil {
		return err
	}

	workspace.SetSessionWorkingDir(sessionID, "Workflow/")
	workspace.SetSessionFolderGuard(sessionID,
		[]string{"Workflow/", "Chats/", "Downloads/", "Plans/", "skills/", "subagents/"},
		[]string{"Chats/", "Downloads/"},
	)

	return nil
}

// workshopExtractLLM extracts an AgentLLMConfig from preset config, with legacy fallback.
// Returns nil if neither specific nor legacy values are set.
func workshopExtractLLM(specific *database.AgentLLMConfig, legacyProvider, legacyModelID string) *todo_creation_human.AgentLLMConfig {
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

// workshopConvertFallbacks converts database fallbacks to step_based_workflow fallbacks.
func workshopConvertFallbacks(fallbacks []database.AgentLLMFallback) []todo_creation_human.AgentLLMFallback {
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
