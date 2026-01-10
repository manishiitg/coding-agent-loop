package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	orchtypes "mcp-agent-builder-go/agent_go/pkg/orchestrator/types"
	unifiedevents "mcpagent/events"
	"mcpagent/executor"
	"mcpagent/llm"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/pkg/logger"

	"github.com/joho/godotenv"

	eventbridge "mcp-agent-builder-go/agent_go/cmd/server/event_bridge"
	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	mcpagent "mcpagent/agent"
	"strconv"
)

// extractWorkspacePathFromObjective extracts the workspace path from the objective string
// Looks for pattern: "📁 Files in context: Workflow/[FolderName]"
func extractWorkspacePathFromObjective(objective string) string {
	// Look for pattern: "📁 Files in context: Workflow/[FolderName]"
	// This is the standard pattern used by workflow orchestrator
	prefix := "📁 Files in context: "
	if idx := strings.Index(objective, prefix); idx != -1 {
		// Find the start of the workspace path
		start := idx + len(prefix)
		// Find the end of the workspace path (typically before a newline or end of string)
		end := strings.Index(objective[start:], "\n")
		if end == -1 {
			return strings.TrimSpace(objective[start:])
		}
		return strings.TrimSpace(objective[start : start+end])
	}
	return ""
}

// extractRootCauseError returns the raw error message without any processing
// It unwraps the error chain to find the deepest/most specific error
func extractRootCauseError(err error) string {
	if err == nil {
		return "unknown error"
	}

	// Unwrap the error chain to find the deepest error (the actual root cause)
	currentErr := err
	deepestErr := err
	maxDepth := 20 // Limit depth to prevent infinite loops

	for i := 0; i < maxDepth; i++ {
		// Try to unwrap using errors.Unwrap
		unwrapped := errors.Unwrap(currentErr)
		if unwrapped == nil {
			break
		}
		deepestErr = unwrapped
		currentErr = unwrapped
	}

	// Return the raw error message from the deepest error (no pattern matching, no filtering)
	return deepestErr.Error()
}

// createCustomTools creates workspace and human tools for orchestrator/workflow agents
// includeHumanTools: if true, includes human tools (for workflow mode); if false, only workspace tools (for chat mode)
// Returns: tools, executors, and a map of tool names to their categories
// All tools from CreateWorkspaceTools() get category "workspace"
// All tools from CreateHumanTools() get category "human"
func createCustomTools(includeHumanTools bool) ([]llmtypes.Tool, map[string]interface{}, map[string]string) {
	// Create workspace tools (always included)
	workspaceCategory := virtualtools.GetWorkspaceToolCategory()
	workspaceTools := virtualtools.CreateWorkspaceTools()
	workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()

	// Initialize with workspace tools
	allTools := workspaceTools
	allExecutors := make(map[string]interface{})
	for name, executor := range workspaceExecutors {
		allExecutors[name] = executor
	}

	// Build category map - start with workspace tools
	toolCategories := make(map[string]string)
	for _, tool := range workspaceTools {
		if tool.Function != nil {
			toolCategories[tool.Function.Name] = workspaceCategory
		}
	}

	// Conditionally include human tools (only for workflow mode)
	if includeHumanTools {
		humanCategory := virtualtools.GetHumanToolCategory()
		humanTools := virtualtools.CreateHumanTools()
		humanExecutors := virtualtools.CreateHumanToolExecutors()

		// Combine workspace and human tools
		allTools = append(allTools, humanTools...)
		for name, executor := range humanExecutors {
			allExecutors[name] = executor
		}

		// Assign category to all tools from CreateHumanTools()
		for _, tool := range humanTools {
			if tool.Function != nil {
				toolCategories[tool.Function.Name] = humanCategory
			}
		}
	}

	return allTools, allExecutors, toolCategories
}

// wrapExecutorsWithChatModeFolderGuard wraps workspace tool executors to block Workflow/ folder WRITE access in chat mode
// This creates a wrapper that:
// 1. ALLOWS read access to Workflow/ (list, search, read operations)
// 2. BLOCKS write access to Workflow/ (update, delete, move, shell commands)
// 3. BLOCKS git commands (can leak data through .git/ database)
func wrapExecutorsWithChatModeFolderGuard(executors map[string]func(ctx context.Context, args map[string]interface{}) (string, error)) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Blocked paths for WRITE operations - Workflow/ folder is read-only in chat mode
	blockedWritePaths := []string{"Workflow/", "Workflow"}

	// Write tools that should be blocked for Workflow/ paths
	writeTools := map[string]bool{
		"update_workspace_file":     true,
		"delete_workspace_file":     true,
		"move_workspace_file":       true,
		"diff_patch_workspace_file": true,
	}

	// Path parameters to check for write tools
	writePathParams := []string{"filepath", "source_filepath", "destination_filepath"}

	wrappedExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	for toolName, executor := range executors {
		toolNameCopy := toolName
		originalExecutor := executor

		wrappedExecutor := func(ctx context.Context, args map[string]interface{}) (string, error) {
			// For shell commands, block git commands and commands referencing Workflow/
			// Shell commands can potentially write, so we block Workflow/ access entirely
			if toolNameCopy == "execute_shell_command" {
				if cmdValue, exists := args["command"]; exists {
					if cmdStr, ok := cmdValue.(string); ok {
						cmdLower := strings.ToLower(cmdStr)

						// Block git commands entirely - they can leak/modify Workflow data through .git/ database
						if strings.HasPrefix(cmdLower, "git ") || strings.Contains(cmdLower, " git ") ||
							strings.Contains(cmdLower, "&& git ") || strings.Contains(cmdLower, "; git ") ||
							strings.Contains(cmdLower, "| git ") || strings.Contains(cmdLower, "|| git ") {
							log.Printf("[CHAT MODE FOLDER GUARD] Blocked git command (can modify restricted folder): %s", cmdStr)
							return "", fmt.Errorf("access denied: git commands are not allowed in chat mode (they can modify restricted folders)")
						}

						// Block shell commands referencing Workflow/ (can't distinguish read/write at shell level)
						for _, blockedPath := range blockedWritePaths {
							blockedLower := strings.ToLower(strings.TrimSuffix(blockedPath, "/"))
							if strings.Contains(cmdLower, blockedLower+"/") ||
								strings.Contains(cmdLower, blockedLower+" ") ||
								strings.Contains(cmdLower, " "+blockedLower) ||
								strings.Contains(cmdLower, "/"+blockedLower) ||
								strings.HasSuffix(cmdLower, blockedLower) {
								log.Printf("[CHAT MODE FOLDER GUARD] Blocked shell command referencing '%s': %s", blockedPath, cmdStr)
								return "", fmt.Errorf("access denied: shell commands cannot reference '%s' (use workspace tools for read access)", blockedPath)
							}
						}
					}
				}
				// Inject blocked paths for kernel-level sandboxing (defense in depth for writes)
				log.Printf("[CHAT MODE FOLDER GUARD] Shell command - kernel-level sandbox will block writes to: %v", blockedWritePaths)
				ctx = context.WithValue(ctx, virtualtools.FolderGuardBlockedPathsKey, blockedWritePaths)
			}

			// For WRITE tools only, check path parameters for Workflow/ paths
			if writeTools[toolNameCopy] {
				for _, paramName := range writePathParams {
					if paramValue, exists := args[paramName]; exists {
						if pathStr, ok := paramValue.(string); ok && pathStr != "" {
							for _, blockedPath := range blockedWritePaths {
								if strings.HasPrefix(pathStr, blockedPath) {
									log.Printf("[CHAT MODE FOLDER GUARD] Blocked WRITE to '%s' for tool %s - Workflow/ is read-only in chat mode", pathStr, toolNameCopy)
									return "", fmt.Errorf("access denied: cannot write to '%s' (Workflow/ folder is read-only in chat mode)", pathStr)
								}
							}
						}
					}
				}
			}

			// READ tools (list, search, read_workspace_file) are ALLOWED for Workflow/
			// No blocking or context injection needed for read operations

			// Call original executor
			return originalExecutor(ctx, args)
		}

		wrappedExecutors[toolNameCopy] = wrappedExecutor
	}

	log.Printf("[CHAT MODE FOLDER GUARD] Wrapped %d executors - Workflow/ is READ-ONLY (writes blocked)", len(wrappedExecutors))
	return wrappedExecutors
}

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
	AgentMode     string   `json:"agent_mode"` // Add agent mode configuration

	// Structured Output LLM Configuration
	StructuredOutputProvider string  `json:"structured_output_provider"`
	StructuredOutputModel    string  `json:"structured_output_model"`
	StructuredOutputTemp     float64 `json:"structured_output_temperature"`
}

// ActiveSessionInfo represents an active session for page refresh recovery
type ActiveSessionInfo struct {
	SessionID    string    `json:"session_id"`
	AgentMode    string    `json:"agent_mode"`
	Status       string    `json:"status"` // "running", "paused", "completed"
	LastActivity time.Time `json:"last_activity"`
	CreatedAt    time.Time `json:"created_at"`
	Query        string    `json:"query,omitempty"`
	LLMGuidance  string    `json:"llm_guidance,omitempty"` // LLM guidance message for this session
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

	// Orchestrator objects in memory for guidance injection
	workflowOrchestrators    map[string]orchestrator.Orchestrator
	workflowOrchestratorsMux sync.RWMutex

	toolStatus    map[string]ToolStatus
	enabledTools  map[string][]string // queryID/sessionID -> enabled tool names
	toolStatusMux sync.RWMutex
	mcpConfig     *mcpclient.MCPConfig

	// Background tool discovery
	discoveryRunning bool
	discoveryMux     sync.RWMutex
	lastDiscovery    time.Time
	discoveryTicker  *time.Ticker

	// Logger for structured logging
	logger loggerv2.Logger
}

// QueryRequest represents an agent query request
type QueryRequest struct {
	Query          string                  `json:"query"`
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
	// Workspace access configuration
	EnableWorkspaceAccess *bool `json:"enable_workspace_access,omitempty"` // Enable/disable workspace file access tools (nil = inherit default, true/false = explicit override)
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
	SessionID string `json:"session_id"`
	Guidance  string `json:"guidance"`
}

// LLMGuidanceResponse represents the response for LLM guidance operations
type LLMGuidanceResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Guidance  string `json:"guidance,omitempty"`
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
	ServerCmd.Flags().String("agent-mode", "simple", "Agent mode (simple)")

	// Structured Output LLM flags
	ServerCmd.Flags().String("structured-output-provider", "", "Structured output LLM provider (uses main provider if empty)")
	ServerCmd.Flags().String("structured-output-model", "", "Structured output model ID (uses main model if empty)")
	ServerCmd.Flags().Float64("structured-output-temp", 0.0, "Structured output temperature (uses main temperature if 0)")

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
		AgentMode:     viper.GetString("agent-mode"), // Bind agent mode flag

		// Structured Output LLM Configuration
		StructuredOutputProvider: viper.GetString("structured-output-provider"),
		StructuredOutputModel:    viper.GetString("structured-output-model"),
		StructuredOutputTemp:     viper.GetFloat64("structured-output-temp"),
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

	// Set agent mode from environment variable if not set via command line
	if config.AgentMode == "" {
		if envMode := os.Getenv("ORCHESTRATOR_AGENT_MODE"); envMode != "" {
			config.AgentMode = envMode
		} else {
			config.AgentMode = "simple" // Default to simple agent
		}
	}

	// Set structured output LLM configuration from environment variables if not set via command line
	if config.StructuredOutputProvider == "" {
		if envProvider := os.Getenv("ORCHESTRATOR_STRUCTURED_OUTPUT_PROVIDER"); envProvider != "" {
			config.StructuredOutputProvider = envProvider
		}
	}
	if config.StructuredOutputModel == "" {
		if envModel := os.Getenv("ORCHESTRATOR_STRUCTURED_OUTPUT_MODEL"); envModel != "" {
			config.StructuredOutputModel = envModel
		}
	}
	if config.StructuredOutputTemp == 0.0 {
		if envTemp := os.Getenv("ORCHESTRATOR_STRUCTURED_OUTPUT_TEMPERATURE"); envTemp != "" {
			if temp, err := strconv.ParseFloat(envTemp, 64); err == nil {
				config.StructuredOutputTemp = temp
			}
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
		log.Fatalf("Invalid provider: %w", err)
	}

	// Set default model if not specified
	if config.ModelID == "" {
		config.ModelID = llm.GetDefaultModel(llmProvider)
	}

	fmt.Printf("🚀 Starting Streaming API Server\n")
	fmt.Printf("📡 Host: %s:%d\n", config.Host, config.Port)
	fmt.Printf("🤖 Primary Provider: %s | Model: %s\n", config.Provider, config.ModelID)
	fmt.Printf("🧠 Agent Mode: %s\n", config.AgentMode)

	// Show tracing configuration
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}
	fmt.Printf("📊 Tracing: %s\n", tracingProvider)

	// Show structured output LLM configuration
	if config.StructuredOutputProvider != "" || config.StructuredOutputModel != "" {
		provider := config.StructuredOutputProvider
		model := config.StructuredOutputModel
		temp := config.StructuredOutputTemp

		if provider == "" {
			provider = config.Provider
		}
		if model == "" {
			model = config.ModelID
		}
		if temp == 0.0 {
			temp = config.Temperature
		}

		fmt.Printf("🔧 Structured Output LLM: %s | %s | temp=%.2f\n", provider, model, temp)
	}

	fmt.Printf("🌐 CORS Origins: %v\n", config.CORSOrigins)
	fmt.Printf("📁 Config: %s\n", config.MCPConfigPath)

	// Create streaming API server
	configPath := config.MCPConfigPath
	mcpConfig, err := mcpclient.LoadConfig(configPath, nil) // Logger not yet available, will be created later
	if err != nil {
		log.Fatalf("Failed to load MCP config: %w", err)
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
		dbPath := viper.GetString("db-path")
		if dbPath == "" {
			dbPath = "/app/chat_history.db" // Default SQLite database path
		}
		chatDB, err = database.NewSQLiteDB(dbPath)
		connInfo = fmt.Sprintf("SQLite (%s)", dbPath)
	}

	if err != nil {
		log.Fatalf("Failed to initialize chat history database: %w", err)
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
		logger:                       createServerLogger(),
		// Initialize background discovery fields
		discoveryRunning: false,
		lastDiscovery:    time.Time{},
		discoveryTicker:  nil,
		// Initialize active session tracking
		activeSessions: make(map[string]*ActiveSessionInfo),
		// Initialize orchestrator storage
		workflowOrchestrators: make(map[string]orchestrator.Orchestrator),
		// Initialize workflow step ID storage
		workflowStepIDs: make(map[string]string),
	}

	// Setup routes
	router := mux.NewRouter()

	// CORS middleware
	router.Use(api.corsMiddleware)

	// API routes
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.HandleFunc("/query", api.handleQuery).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/health", api.handleHealth).Methods("GET")
	apiRouter.HandleFunc("/capabilities", api.handleCapabilities).Methods("GET")
	apiRouter.HandleFunc("/llm-config/defaults", api.handleGetLLMDefaults).Methods("GET")
	apiRouter.HandleFunc("/llm-config/models/metadata", api.handleGetModelMetadata).Methods("GET")
	apiRouter.HandleFunc("/llm-config/validate-key", api.handleValidateAPIKey).Methods("POST")
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
	apiRouter.HandleFunc("/mcp/execute", executorHandlers.HandleMCPExecute).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/custom/execute", executorHandlers.HandleCustomExecute).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/virtual/execute", executorHandlers.HandleVirtualExecute).Methods("POST", "OPTIONS")

	// MCP Config API routes (from mcp_config_routes.go)
	apiRouter.HandleFunc("/mcp-config", api.handleGetMCPConfig).Methods("GET")
	apiRouter.HandleFunc("/mcp-config", api.handleSaveMCPConfig).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/discover", api.handleDiscoverServers).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/status", api.handleGetMCPConfigStatus).Methods("GET")

	// OAuth API routes (from oauth_routes.go)
	apiRouter.HandleFunc("/oauth/start", api.handleOAuthStart).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/oauth/callback", api.handleOAuthCallback).Methods("GET")
	apiRouter.HandleFunc("/oauth/status", api.handleOAuthStatus).Methods("GET")
	apiRouter.HandleFunc("/oauth/logout", api.handleOAuthLogout).Methods("POST", "OPTIONS")

	// Observer APIs removed - events are now stored by sessionID, no observers needed

	// Active Session API routes (from polling.go)
	apiRouter.HandleFunc("/sessions/active", api.handleGetActiveSessions).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/events", api.handleGetSessionEvents).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/reconnect", api.handleReconnectSession).Methods("POST")
	apiRouter.HandleFunc("/sessions/{session_id}/status", api.handleGetSessionStatus).Methods("GET")

	// LLM Guidance API routes
	apiRouter.HandleFunc("/sessions/{session_id}/llm-guidance", api.handleSetLLMGuidance).Methods("POST", "OPTIONS")

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

	// Preset Queries API routes
	PresetQueryRoutes(router, chatDB)

	// Slack Feedback API routes
	SlackFeedbackRoutes(router, api, chatDB)

	// Set activity callback for event store to update session LastActivity when events are added
	eventStore.SetActivityCallback(func(sessionID string) {
		api.updateSessionActivity(sessionID)
	})

	// Start background cleanup goroutine to mark inactive sessions (10 minute timeout)
	go api.cleanupInactiveSessions()

	// Workflow API routes
	apiRouter.HandleFunc("/workflow/create", api.handleCreateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/status", api.handleGetWorkflowStatus).Methods("GET")
	apiRouter.HandleFunc("/workflow/update", api.handleUpdateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/constants", orchtypes.HandleWorkflowConstants).Methods("GET")

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

	// Plan and Step Config API routes
	apiRouter.HandleFunc("/workflow/plan/update-step", api.handleUpdatePlanStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/update-step-config", api.handleUpdateStepConfig).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/batch-update-steps", api.handleBatchUpdateSteps).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/delete-step", api.handleDeleteStep).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/plan/add-step", api.handleAddStep).Methods("POST", "OPTIONS")

	// Static file serving (for frontend)
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		WriteTimeout: time.Second * 30,  // Increased for streaming
		ReadTimeout:  time.Second * 30,  // Increased for streaming
		IdleTimeout:  time.Second * 300, // 5 min idle timeout to prevent early closes during long queries
		Handler:      router,
	}

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %w", err)
		}
	}()

	fmt.Printf("✅ Server started on %s:%d\n", config.Host, config.Port)
	fmt.Printf("🔗 API endpoint: http://%s:%d/api/query\n", config.Host, config.Port)
	fmt.Printf("📡 Polling API: http://%s:%d/api/sessions/{session_id}/events\n", config.Host, config.Port)

	// Initialize tool cache on server startup
	fmt.Printf("🔄 Initializing tool cache on server startup...\n")
	api.initializeToolCache()

	// Wait for interrupt signal to gracefully shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c

	fmt.Println("\n🛑 Shutting down server...")

	// Stop background discovery
	fmt.Println("⏹️ Stopping background tool discovery...")
	api.stopPeriodicRefresh()

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Shutdown server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %w", err)
	}

	fmt.Println("✅ Server shutdown complete")
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
		"status": "healthy",
		"time":   time.Now(),
		"config": map[string]interface{}{
			"provider":         api.config.Provider,
			"model":            api.config.ModelID,
			"temperature":      api.config.Temperature,
			"max_turns":        api.config.MaxTurns,
			"tracing_provider": tracingProvider,
		},
	})
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
		"servers": []string{},
	})
}

// handleGetLLMDefaults returns default LLM configurations from environment variables
func (api *StreamingAPI) handleGetLLMDefaults(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request for LLM defaults")

	defaults := llm.GetLLMDefaults()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defaults)
}

// handleValidateAPIKey validates API keys for OpenRouter, OpenAI, Bedrock, Vertex, and Anthropic
func (api *StreamingAPI) handleValidateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req llm.APIKeyValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode API key validation request: %w", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Received API key validation request for provider: %s", req.Provider)

	response := llm.ValidateAPIKey(req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
		errorMsg := fmt.Sprintf("Invalid request body: %w", err)
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Query == "" {
		errorMsg := "Query is required"
		http.Error(w, errorMsg, http.StatusBadRequest)
		return
	}

	// Record start time for duration calculation
	startTime := time.Now()

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

	// Set agent execution LLM defaults: API request takes precedence, then environment variables, then server config, then fallback to Bedrock
	agentProvider := req.Provider // API request takes highest priority
	if agentProvider == "" {
		agentProvider = os.Getenv("AGENT_PROVIDER") // Environment variable as fallback
	}
	if agentProvider == "" {
		agentProvider = api.config.Provider // Server config as fallback
	}
	if agentProvider == "" {
		agentProvider = "bedrock" // Default fallback
	}

	// Set agent model: API request takes precedence, then environment variables, then server config
	agentModel := req.ModelID // API request takes highest priority
	if agentModel == "" {
		agentModel = os.Getenv("AGENT_MODEL") // Environment variable as fallback
	}
	if agentModel == "" {
		agentModel = api.config.ModelID // Server config as fallback
	}
	if agentModel == "" && agentProvider == "bedrock" {
		agentModel = os.Getenv("BEDROCK_PRIMARY_MODEL") // Use .env configuration
	}
	req.Provider = agentProvider
	req.ModelID = agentModel

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

	var serverList string
	// Check for explicit "NO_SERVERS" request (pure LLM mode, no tools)
	if len(selectedServers) == 1 && selectedServers[0] == mcpclient.NoServers {
		// Keep NoServers constant as-is - this will be handled by integration code
		serverList = mcpclient.NoServers
	} else {
		// Default to all servers if none specified
		if len(selectedServers) == 0 {
			selectedServers = []string{"all"}
		}

		// Convert server array to comma-separated string for agent compatibility
		serverList = strings.Join(selectedServers, ",")
	}

	// Extract sessionID from header/cookie or fallback to queryID
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = queryID // fallback: use queryID as sessionID if not provided
	}

	// Create or get chat session for this query
	// The agent will modify the session ID to agent-init-{sessionID}-{timestamp}
	// So we need to create the chat session with the original sessionID
	// and the events will use the modified sessionID
	chatSession, err := api.chatDB.GetChatSession(r.Context(), sessionID)
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
		hasConfig := len(req.Servers) > 0 || len(req.EnabledServers) > 0 || req.UseCodeExecutionMode || req.EnableContextSummarization != nil || req.Provider != "" || req.ModelID != "" || req.LLMConfig != nil
		if hasConfig {
			config := &database.ChatSessionConfig{
				SelectedServers:      req.Servers,
				EnabledServers:       req.EnabledServers,
				UseCodeExecutionMode: req.UseCodeExecutionMode,
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

			var err error
			configJSON, err = config.ToJSON()
			if err != nil {
				log.Printf("[CONFIG DEBUG] Failed to marshal config: %v", err)
				configJSON = nil
			}
		}

		chatSession, err = api.chatDB.CreateChatSession(r.Context(), &database.CreateChatSessionRequest{
			SessionID:     sessionID,
			Title:         title,
			AgentMode:     req.AgentMode,
			PresetQueryID: req.PresetQueryID,
			Config:        configJSON,
		})
		if err != nil {
			log.Printf("[DATABASE DEBUG] Failed to create chat session: %w", err)
			// Continue without chat session - events won't be stored but query can proceed
		}
	} else {
		// Reactivate session if it was stopped/completed/error - update status to "active" for new query
		if chatSession.Status == "stopped" || chatSession.Status == "completed" || chatSession.Status == "error" {
			updateReq := &database.UpdateChatSessionRequest{
				Status:      "active",
				CompletedAt: nil, // Clear completion timestamp when reactivating
			}
			_, err := api.chatDB.UpdateChatSession(r.Context(), sessionID, updateReq)
			if err != nil {
				// Continue with existing session status
			}

			// Initialize EventStore for reactivated session to ensure new events are stored correctly
			// Calculate existing event count to use as baseIndex for polling
			var baseIndex int
			existingEvents, err := api.chatDB.GetEventsBySession(r.Context(), sessionID, 1000000, 0)
			if err == nil {
				baseIndex = len(existingEvents)
				log.Printf("[SESSION REACTIVATION] Found %d existing events for session %s, setting baseIndex", baseIndex, sessionID)
			} else {
				log.Printf("[SESSION REACTIVATION] Failed to count existing events for session %s: %v", sessionID, err)
			}
			api.eventStore.InitializeSession(sessionID, baseIndex)
		}
	}

	// Track active session for page refresh recovery (no observer needed)
	api.trackActiveSession(sessionID, req.AgentMode, req.Query)

	// Create fresh agent for this request
	// Use LLM configuration from request if provided, otherwise use request defaults
	var finalProvider string
	var finalModelID string
	var fallbacks []agent.FallbackModel

	if req.LLMConfig != nil {
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
		// Note: ChatDB is set to nil - workflow events are stored in memory only (for polling API)
		// Chat history database storage is disabled for workflows to reduce database load
		workflowEventBridge := &eventbridge.WorkflowEventBridge{
			BaseEventBridge: &eventbridge.BaseEventBridge{
				EventStore: api.eventStore,
				SessionID:  sessionID,
				Logger:     api.logger,
				ChatDB:     nil, // Disable database storage for workflows
				BridgeName: "workflow",
			},
		}

		// Create custom tools for workflow agents (workspace tools + human tools)
		// Workflow agents can be Simple or ReAct agents, tools are registered based on mode
		// TODO: Memory tools removed from workflow - only needed for individual React agents
		// memoryTools := virtualtools.CreateMemoryTools()
		// memoryExecutors := virtualtools.CreateMemoryToolExecutors()
		allTools, allExecutors, toolCategories := createCustomTools(true) // Include human tools for workflow mode

		// Load selected tools, code execution mode, and preset LLM config from preset if available (for workflow agents)
		var selectedTools []string
		var useCodeExecutionMode bool
		var presetLLMConfig *database.PresetLLMConfig
		if req.PresetQueryID != "" {
			ctx := context.Background()
			preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
			if err == nil {
				// Load selected tools
				if preset.SelectedTools != "" {
					if err := json.Unmarshal([]byte(preset.SelectedTools), &selectedTools); err != nil {
						log.Printf("[TOOLS] Failed to parse selected tools from preset: %w", err)
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
							if presetLLMConfig.ExecutionLLM != nil {
								log.Printf("[PRESET LLM DEBUG] ExecutionLLM: provider=%s, model=%s", presetLLMConfig.ExecutionLLM.Provider, presetLLMConfig.ExecutionLLM.ModelID)
							}
							if presetLLMConfig.ValidationLLM != nil {
								log.Printf("[PRESET LLM DEBUG] ValidationLLM: provider=%s, model=%s", presetLLMConfig.ValidationLLM.Provider, presetLLMConfig.ValidationLLM.ModelID)
							}
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

		// Create workflow orchestrator for this request
		// Note: req.MaxTurns is already defaulted to 100 earlier in the handler
		workflowOrchestrator, err := orchtypes.NewWorkflowOrchestrator(
			req.Provider,         // provider
			req.ModelID,          // model
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
			req.LLMConfig,        // llmConfig
			req.MaxTurns,         // maxTurns (defaults to 100 if not provided)
			toolCategories,       // NEW: toolCategories
			presetLLMConfig,      // preset LLM config for agent defaults
		)
		if err != nil {
			log.Printf("[WORKFLOW ERROR] Failed to create workflow orchestrator: %w", err)
			http.Error(w, fmt.Sprintf("Failed to create workflow orchestrator: %w", err), http.StatusInternalServerError)
			return
		}

		// Store workflow orchestrator for guidance injection
		api.storeWorkflowOrchestrator(sessionID, workflowOrchestrator)

		// Create a cancellable context for workflow execution using background context
		// This prevents the workflow from being cancelled when the HTTP request ends
		workflowCtx, workflowCancel := context.WithCancel(context.Background())

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
			http.Error(w, fmt.Sprintf("Failed to encode response: %w", err), http.StatusInternalServerError)
			return
		}

		// Execute workflow asynchronously
		go func() {
			defer func() {
				// Clean up the cancel function when done (keyed by queryID)
				api.workflowOrchestratorContextMux.Lock()
				delete(api.workflowOrchestratorContexts, queryID)
				api.workflowOrchestratorContextMux.Unlock()

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
					log.Printf("[WORKFLOW CHECK] Could not check database: %w", err)
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
					workflowObjective = preset.Query
					log.Printf("[WORKFLOW EXECUTION] Using preset objective: %s", workflowObjective)

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

			// Prepare options for the Execute method
			workflowOptions := map[string]interface{}{
				"workflowStatus":  workflowStatus,  // Current workflow status
				"selectedOptions": selectedOptions, // Pass selected options from database
			}
			if stepID != "" {
				workflowOptions["stepId"] = stepID // Pass step ID for step-specific phase execution
			}

			// Pass execution options from frontend if provided
			if req.ExecutionOptions != nil {
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
					SaveValidationResponses:        req.ExecutionOptions.SaveValidationResponses,
					DisableShellExecAccess:         req.ExecutionOptions.DisableShellExecAccess,
					DisableReadImageAccess:         req.ExecutionOptions.DisableReadImageAccess,
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

				// Convert TempOverrideLLM2 if present
				if req.ExecutionOptions.TempOverrideLLM2 != nil {
					controllerOpts.TempOverrideLLM2 = &todo_creation_human.AgentLLMConfig{
						Provider: req.ExecutionOptions.TempOverrideLLM2.Provider,
						ModelID:  req.ExecutionOptions.TempOverrideLLM2.ModelID,
					}
				}

				// Set execution options on the workflow orchestrator
				workflowOrchestrator.SetExecutionOptions(controllerOpts)
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
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (workflow): %w", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status (workflow)", sessionID)
					}
				}
				// --- END: Update chat session status to error ---

				// Update active session status to error
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to error", sessionID)
				api.updateSessionStatus(sessionID, "error")
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
						Status:        "completed",
						CompletedAt:   &completedAt,
						PresetQueryID: "", // Explicitly set to empty string to trigger NULL in database
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, err := api.chatDB.UpdateChatSession(ctx, sessionID, updateReq)
					cancel()
					if err != nil {
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to completed (workflow): %w", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to completed status (workflow)", sessionID)
					}
				}
				// --- END: Update chat session status to completed ---

				// Update active session status to completed
				log.Printf("[WORKFLOW COMPLETION] Updating session %s status to completed", sessionID)
				api.updateSessionStatus(sessionID, "completed")
			}
		}()
		return
	}

	// Return immediate response with query ID
	response := QueryResponse{
		QueryID:   queryID,
		SessionID: sessionID, // Include the actual session ID used for conversation history
		Status:    "started",
		Message:   "Query processing started. Use polling API to get real-time updates.",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %w", err), http.StatusInternalServerError)
		return
	}

	// Don't clear events - let the frontend handle event continuation
	// The deduplication logic in the frontend will handle any duplicates

	// Process the query in the background
	go func() {
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
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error: %w", err)
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

		// Validate provider
		llmProvider, err := llm.ValidateProvider(req.Provider)
		if err != nil {
			sendError(fmt.Sprintf("Invalid provider: %w", err), true)
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
		var presetSetCodeExecutionMode bool // Track if preset explicitly set the value

		if req.PresetQueryID != "" {
			ctx := context.Background()
			preset, err := api.chatDB.GetPresetQuery(ctx, req.PresetQueryID)
			if err == nil {
				if preset.SelectedTools != "" {
					if err := json.Unmarshal([]byte(preset.SelectedTools), &selectedTools); err != nil {
						log.Printf("[TOOLS] Failed to parse selected tools from preset: %w", err)
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

		// Create new agent with streamCtx instead of r.Context()
		log.Printf("[AGENT CONFIG DEBUG] Creating agent with ServerName: %s, UseCodeExecutionMode: %v", serverList, useCodeExecutionMode)
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
			Timeout:            2 * time.Minute,
			ToolTimeout: func() time.Duration {
				// Check environment variable
				if envVal := os.Getenv("TOOL_EXECUTION_TIMEOUT"); envVal != "" {
					if timeout, err := time.ParseDuration(envVal); err == nil && timeout > 0 {
						return timeout
					}
				}
				// Default to 2 minutes if not specified
				return 2 * time.Minute
			}(),
			SelectedTools: selectedTools, // NEW: Pass selected tools

			// Enable smart routing by default for both React and Simple agents
			EnableSmartRouting:     true,
			SmartRoutingMaxTools:   20, // Enable when more than 20 tools
			SmartRoutingMaxServers: 4,  // Enable when more than 4 servers

			// Detailed LLM configuration from frontend (unified fallback structure)
			Fallbacks: fallbacks,
			// Code execution mode: When enabled, only virtual tools are added to LLM
			// MCP tools are accessed via generated Go code using discover_code_files and write_code
			UseCodeExecutionMode: useCodeExecutionMode,
			// Convert API keys from request to LLM format
			APIKeys: func() *llm.ProviderAPIKeys {
				if req.LLMConfig != nil && req.LLMConfig.APIKeys != nil {
					llmKeys := &llm.ProviderAPIKeys{
						OpenRouter: req.LLMConfig.APIKeys.OpenRouter,
						OpenAI:     req.LLMConfig.APIKeys.OpenAI,
						Anthropic:  req.LLMConfig.APIKeys.Anthropic,
						Vertex:     req.LLMConfig.APIKeys.Vertex,
					}
					if req.LLMConfig.APIKeys.Bedrock != nil {
						llmKeys.Bedrock = &llm.BedrockConfig{
							Region: req.LLMConfig.APIKeys.Bedrock.Region,
						}
					}
					return llmKeys
				}
				return nil
			}(),
			// Context summarization configuration
			// Priority: Request > Environment Variable > Default (matches orchestrator defaults)
			EnableContextSummarization: func() bool {
				// If explicitly set in request, use that value
				if req.EnableContextSummarization != nil {
					return *req.EnableContextSummarization
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
				// If explicitly set in request, use that value
				if req.EnableContextEditing != nil {
					return *req.EnableContextEditing
				}
				// Check environment variable
				if envVal := os.Getenv("ENABLE_CONTEXT_EDITING"); envVal == "true" {
					return true
				}
				// Default to enabled (true), can be disabled via ENABLE_CONTEXT_EDITING=false
				return os.Getenv("ENABLE_CONTEXT_EDITING") != "false"
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
			// MCP session ID for connection reuse (e.g., Playwright browser sharing)
			// Use the chat session ID so all agents in the same session share MCP connections
			SessionID: sessionID,
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
		// Create LLM agent wrapper with trace using streamCtx
		llmAgent, err := agent.NewLLMAgentWrapperWithTrace(streamCtx, agentConfig, tracer, traceID, api.logger)
		if err != nil {
			log.Printf("[AGENT DEBUG] Failed to create LLM agent wrapper: %w", err)
			sendError(fmt.Sprintf("Failed to create agent: %w", err), true)
			return
		}

		// Add workspace tools to simple agents (chat mode)
		// This matches how workspace tools are registered in workflow/orchestrator agents
		// This ensures custom tools are available and code generation is triggered
		// Only register workspace tools if workspace access is enabled
		// Note: Frontend always sends enable_workspace_access for chat mode (true/false)
		// Chat mode is detected by: "simple", "" (empty/default), or "chat" agent mode
		// Workflow/orchestrator modes handle workspace tools differently, so exclude them
		isChatMode := req.AgentMode == "simple" || req.AgentMode == "" || req.AgentMode == "chat"
		// log.Printf("[FOLDER GUARD DEBUG] req.AgentMode=%s, isChatMode=%v, GetUnderlyingAgent()!=nil: %v", req.AgentMode, isChatMode, llmAgent.GetUnderlyingAgent() != nil)
		if isChatMode && llmAgent.GetUnderlyingAgent() != nil {
			// Check if workspace access is enabled
			// Default to true for backward compatibility with legacy requests
			// nil = inherit default (true), non-nil = explicit override
			enableWorkspaceAccess := true // Default to enabled for backward compatibility
			if req.EnableWorkspaceAccess != nil {
				enableWorkspaceAccess = *req.EnableWorkspaceAccess
			}

			if enableWorkspaceAccess {
				workspaceTools := virtualtools.CreateWorkspaceTools()
				workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
				_, _, toolCategories := createCustomTools(false) // Get toolCategories map (no human tools for chat mode)

				// Apply folder guard to block Workflow/ folder access in chat mode
				workspaceExecutors = wrapExecutorsWithChatModeFolderGuard(workspaceExecutors)
				log.Printf("[CHAT MODE FOLDER GUARD] Applied Workflow/ folder restriction to chat mode")

				log.Printf("[WORKSPACE TOOLS] Registering %d workspace tools for simple agent (enable_workspace_access: %v)", len(workspaceTools), enableWorkspaceAccess)

				underlyingAgent := llmAgent.GetUnderlyingAgent()
				for _, tool := range workspaceTools {
					if tool.Function == nil {
						log.Printf("[WORKSPACE TOOLS] Warning: Skipping tool with nil Function")
						continue
					}
					toolName := tool.Function.Name
					if executor, exists := workspaceExecutors[toolName]; exists {
						// Convert Parameters to map[string]interface{} using JSON marshal/unmarshal
						// This matches the workflow implementation for consistency
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
							tool.Function.Description,
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
				log.Printf("[WORKSPACE TOOLS] Successfully registered %d workspace tools for simple agent", len(workspaceTools))
			} else {
				log.Printf("[WORKSPACE TOOLS] Skipping workspace tools registration (enable_workspace_access: false)")
			}
		}

		// Add custom agent instructions based on agent mode
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			// Create custom tools for regular agents (workspace tools only, no human tools for chat mode)
			allTools, allExecutors, toolCategories := createCustomTools(false) // No human tools for chat mode

			// Register each custom tool with the agent
			// This will trigger code generation and update the registry
			// Note: Workspace tools are already registered above, skip them in allTools
			registeredCount := 0
			for _, tool := range allTools {
				if tool.Function != nil {
					toolName := tool.Function.Name

					// Skip workspace tools - already registered above
					if toolCategories[toolName] == virtualtools.GetWorkspaceToolCategory() {
						continue
					}

					// Skip human tools - not available in chat mode
					if toolCategories[toolName] == virtualtools.GetHumanToolCategory() {
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

							// Register the tool - this triggers code generation
							if err := underlyingAgent.RegisterCustomTool(
								toolName,
								tool.Function.Description,
								params,
								execFunc,
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

			// Update code execution registry to rebuild system prompt with newly registered tools
			// This ensures human_feedback and workspace tools appear in the system prompt
			if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
				log.Printf("[CUSTOM TOOLS] Warning: Failed to update code execution registry: %v", err)
			}

			// Add base instructions for all agents
			underlyingAgent.AppendSystemPrompt(GetAgentInstructions())
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
		dbEventObserver := database.NewEventDatabaseObserver(api.chatDB)
		log.Printf("[DATABASE DEBUG] Database event observer created successfully for session %s", sessionID)

		// Add event observer directly to the underlying MCP agent since the wrapper's AddEventListener is disabled
		log.Printf("[DATABASE DEBUG] Getting underlying agent for session %s", sessionID)
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			log.Printf("[DATABASE DEBUG] Underlying agent found, adding event observers for session %s", sessionID)
			underlyingAgent.AddEventListener(eventObserver)
			log.Printf("[DATABASE DEBUG] Added in-memory event observer for session %s", sessionID)
			underlyingAgent.AddEventListener(dbEventObserver)
			log.Printf("[DATABASE DEBUG] Added database event observer for session %s", sessionID)
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
		// This prevents the agent from being cancelled when the HTTP request ends
		agentCtx, agentCancel := context.WithCancel(context.Background())

		// Store the cancel function for potential cancellation
		api.agentCancelMux.Lock()
		api.agentCancelFuncs[sessionID] = agentCancel
		api.agentCancelMux.Unlock()

		// Use the enhanced wrapper to get text chunks - events are handled via EventObserver and polling API
		textChan, err := llmAgent.StreamWithEvents(agentCtx, req.Query)
		if err != nil {
			log.Printf("[AGENT DEBUG] llmAgent.StreamWithEvents() error: %w", err)
			sendError(fmt.Sprintf("Failed to start streaming: %w", err), true)
			return
		}
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
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (timeout): %w", err)
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
		log.Printf("[AGENT DEBUG] Streaming loop exited for query %s", queryID)
		log.Printf("[AGENT DEBUG] After streaming loop, streamCtx.Err(): %v", streamCtx.Err())

		// Final save of conversation history (in case streaming was stopped mid-way)
		// This ensures we capture the final state even if streaming was interrupted
		api.conversationMux.Lock()
		api.conversationHistory[sessionID] = llmAgent.GetHistory()
		api.conversationMux.Unlock()
		log.Printf("[CONVERSATION DEBUG] Final save: %d messages to conversation history for session %s", len(llmAgent.GetHistory()), sessionID)

		// Clean up the agent cancel function when streaming is complete
		api.agentCancelMux.Lock()
		delete(api.agentCancelFuncs, sessionID)
		api.agentCancelMux.Unlock()

		// --- BEGIN: Update chat session status to completed ---
		if chatSession != nil {
			// Update session status to completed with completion timestamp
			// Only update status and completed_at to avoid foreign key constraint issues
			completedAt := time.Now()
			updateReq := &database.UpdateChatSessionRequest{
				Status:        "completed",
				CompletedAt:   &completedAt,
				PresetQueryID: "", // Explicitly set to empty string to trigger NULL in database
			}
			_, err := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
			if err != nil {
				log.Printf("[DATABASE DEBUG] Failed to update chat session status to completed: %w", err)
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

	// Cancel agent execution context if it exists
	api.agentCancelMux.Lock()
	if cancelFunc, exists := api.agentCancelFuncs[sessionID]; exists {
		cancelFunc() // Cancel the agent execution
		delete(api.agentCancelFuncs, sessionID)
		log.Printf("[SESSION DEBUG] Cancelled agent execution context for session %s", sessionID)
	}
	api.agentCancelMux.Unlock()

	// Update active session status to stopped
	api.updateSessionStatus(sessionID, "stopped")

	// Note: No regular agent cleanup needed - fresh agents created per request

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
				log.Printf("[SESSION DEBUG] Cancelled workflow execution %s for session %s", qid, sessionID)
			}
		}
		api.workflowOrchestratorContextMux.Unlock()
		log.Printf("[SESSION DEBUG] Cancelled %d workflow execution(s) for session %s", len(queryIDs), sessionID)
	}

	// Clear workflow objective
	api.workflowObjectiveMux.Lock()
	if _, exists := api.workflowObjectives[sessionID]; exists {
		delete(api.workflowObjectives, sessionID)
		log.Printf("[SESSION DEBUG] Cleared workflow objective for session %s", sessionID)
	}
	api.workflowObjectiveMux.Unlock()

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

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Session cleared (conversation history and orchestrator state removed)"))
}

// State management functions removed - orchestrator is now stateless

// createServerLogger creates a logger instance for the server
// This logger writes to logs/server_debug.log (or the file specified via --log-file flag)
func createServerLogger() loggerv2.Logger {
	// Check for log file from flag or environment variable, default to server_debug.log
	logFile := viper.GetString("log-file")
	if logFile == "" {
		logFile = os.Getenv("LOG_FILE")
	}
	if logFile == "" {
		logFile = "logs/server_debug.log"
	}

	serverLogger, err := logger.CreateLogger(logFile, "info", "text", false)
	if err != nil {
		log.Fatalf("Failed to create server logger: %w", err)
	}
	return serverLogger
}

// createLLMLogger creates a separate logger instance for LLM operations
// This logger writes to logs/llm_debug.log to separate LLM logs from server logs
func createLLMLogger() loggerv2.Logger {
	llmLogger, err := logger.CreateLogger("logs/llm_debug.log", "debug", "text", false)
	if err != nil {
		log.Fatalf("Failed to create LLM logger: %w", err)
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

		session, err := db.CreateChatSession(r.Context(), &req)
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

		sessions, total, err := db.ListChatSessions(r.Context(), limit, offset, presetQueryIDPtr, nil)
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

		session, err := db.GetChatSession(r.Context(), sessionID)
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

		err := db.DeleteChatSession(r.Context(), sessionID)
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

			// Debug: Log first event structure to verify JSON serialization
			if i == 0 && convertedEvent.Data != nil {
				// Marshal to JSON to see actual structure
				if jsonBytes, err := json.Marshal(convertedEvent); err == nil {
					jsonStr := string(jsonBytes)
					maxLen := 500
					if len(jsonStr) > maxLen {
						jsonStr = jsonStr[:maxLen] + "..."
					}
					log.Printf("[CHAT_HISTORY DEBUG] First event JSON structure: %s", jsonStr)
				}

				// Check if GenericEventData is being used correctly
				if genericData, ok := convertedEvent.Data.Data.(*unifiedevents.GenericEventData); ok {
					if dataBytes, err := json.Marshal(genericData); err == nil {
						dataStr := string(dataBytes)
						maxLen := 300
						if len(dataStr) > maxLen {
							dataStr = dataStr[:maxLen] + "..."
						}
						log.Printf("[CHAT_HISTORY DEBUG] GenericEventData structure: %s", dataStr)
					}
					keys := make([]string, 0, len(genericData.Data))
					for k := range genericData.Data {
						keys = append(keys, k)
					}
					log.Printf("[CHAT_HISTORY DEBUG] GenericEventData.Data keys: %v", keys)
				}
			}

			// Debug: Log user_message events specifically
			if convertedEvent.Type == "user_message" && convertedEvent.Data != nil {
				if genericData, ok := convertedEvent.Data.Data.(*unifiedevents.GenericEventData); ok {
					if content, hasContent := genericData.Data["content"]; hasContent {
						contentStr := fmt.Sprintf("%v", content)
						if len(contentStr) > 100 {
							contentStr = contentStr[:100] + "..."
						}
						log.Printf("[CHAT_HISTORY DEBUG] user_message event: hasContent=true, content=%s", contentStr)
					} else {
						keys := make([]string, 0, len(genericData.Data))
						for k := range genericData.Data {
							keys = append(keys, k)
						}
						log.Printf("[CHAT_HISTORY DEBUG] user_message event: hasContent=false, dataKeys=%v", keys)
					}
				}
			}

			convertedEvents = append(convertedEvents, *convertedEvent)
		}

		log.Printf("[CHAT_HISTORY] Converted %d events: total=%d, converted=%d, parse_errors=%d", len(dbEvents), len(dbEvents), len(convertedEvents), parseErrors)

		// Get total count
		totalCount, err := db.GetEventsBySession(r.Context(), sessionID, 0, 0)
		total := len(dbEvents)
		if err == nil && len(totalCount) > 0 {
			if limit == 0 || len(dbEvents) == limit {
				allEvents, err := db.GetEventsBySession(r.Context(), sessionID, 1000000, 0)
				if err == nil {
					total = len(allEvents)
				}
			} else {
				if len(dbEvents) < limit {
					total = offset + len(dbEvents)
				}
			}
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

// --- ACTIVE SESSION MANAGEMENT ---

// trackActiveSession tracks a new active session
func (api *StreamingAPI) trackActiveSession(sessionID, agentMode, query string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	api.activeSessions[sessionID] = &ActiveSessionInfo{
		SessionID:    sessionID,
		AgentMode:    agentMode,
		Status:       "running",
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
		Query:        query,
	}

	log.Printf("[ACTIVE_SESSION] Tracked active session: %s (mode: %s)", sessionID, agentMode)
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
			Status:        status,
			CompletedAt:   completedAt,
			PresetQueryID: "", // Explicitly set to empty string to trigger NULL in database
		})
		if err != nil {
			log.Printf("[ACTIVE_SESSION] Failed to update database for session %s: %v", sessionID, err)
		} else {
			log.Printf("[ACTIVE_SESSION] Successfully updated database for session %s status to: %s", sessionID, status)
		}

		// Remove completed sessions from activeSessions map
		if status == "completed" {
			api.activeSessionsMux.Lock()
			delete(api.activeSessions, sessionID)
			api.activeSessionsMux.Unlock()
			log.Printf("[ACTIVE_SESSION] Removed completed session %s from activeSessions", sessionID)
		}
	}()
}

// getActiveSession retrieves an active session by ID
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
	sessions := make([]*ActiveSessionInfo, 0, len(api.activeSessions))

	for _, session := range api.activeSessions {
		// Only return sessions that are running and have been active within the last 10 minutes
		if session.Status == "running" && now.Sub(session.LastActivity) < inactivityTimeout {
			sessions = append(sessions, session)
		}
	}

	return sessions
}

// cleanupInactiveSessions runs periodically to mark sessions as inactive if no events for 10 minutes
func (api *StreamingAPI) cleanupInactiveSessions() {
	ticker := time.NewTicker(2 * time.Minute) // Check every 2 minutes
	defer ticker.Stop()

	for range ticker.C {
		api.activeSessionsMux.Lock()
		now := time.Now()
		inactivityTimeout := 10 * time.Minute
		sessionsToMarkInactive := make([]string, 0)

		for sessionID, session := range api.activeSessions {
			// Mark as inactive if no activity for 10 minutes and status is still "running"
			if session.Status == "running" && now.Sub(session.LastActivity) >= inactivityTimeout {
				sessionsToMarkInactive = append(sessionsToMarkInactive, sessionID)
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
		http.Error(w, fmt.Sprintf("Invalid request body: %w", err), http.StatusBadRequest)
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

// handleSubmitHumanFeedback handles human feedback submission
func (api *StreamingAPI) handleSubmitHumanFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req HumanFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %w", err), http.StatusBadRequest)
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
