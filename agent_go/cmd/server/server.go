package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tmc/langchaingo/llms"

	"mcp-agent/agent_go/internal/events"
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	agent "mcp-agent/agent_go/pkg/agentwrapper"
	"mcp-agent/agent_go/pkg/database"
	unifiedevents "mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpclient"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	orchtypes "mcp-agent/agent_go/pkg/orchestrator/types"

	"mcp-agent/agent_go/pkg/logger"

	"github.com/joho/godotenv"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
	mcpagent "mcp-agent/agent_go/pkg/mcpagent"
	"strconv"
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
- Multi-provider LLM support (Bedrock, OpenAI)
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

// StoredOrchestratorState represents stored state for any orchestrator type
type StoredOrchestratorState struct {
	Type          string                       `json:"type"` // "planner" or "workflow"
	PlannerState  *orchtypes.OrchestratorState `json:"planner_state,omitempty"`
	WorkflowState *orchtypes.WorkflowState     `json:"workflow_state,omitempty"`
	StoredAt      time.Time                    `json:"stored_at"`
}

// ActiveSessionInfo represents an active session for page refresh recovery
type ActiveSessionInfo struct {
	SessionID    string    `json:"session_id"`
	ObserverID   string    `json:"observer_id"`
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

	// Orchestrator sessions: sessionID -> *PlannerOrchestrator (removed legacy)
	// orchestrators   map[string]*orchtypes.PlannerOrchestrator
	orchestratorMux sync.RWMutex

	// Orchestrator contexts for cancellation: sessionID -> context.CancelFunc
	orchestratorContexts   map[string]context.CancelFunc
	orchestratorContextMux sync.RWMutex

	// Workflow orchestrator sessions: sessionID -> *WorkflowOrchestrator

	// Workflow orchestrator contexts for cancellation: sessionID -> context.CancelFunc
	workflowOrchestratorContexts   map[string]context.CancelFunc
	workflowOrchestratorContextMux sync.RWMutex

	// Workflow objectives: sessionID -> objective
	workflowObjectives   map[string]string
	workflowObjectiveMux sync.RWMutex

	// Conversation history storage: sessionID -> conversation history
	conversationHistory map[string][]llms.MessageContent
	conversationMux     sync.RWMutex

	// Orchestrator state storage: sessionID -> orchestrator state (supports both planner and workflow)
	orchestratorStates   map[string]*StoredOrchestratorState
	orchestratorStateMux sync.RWMutex

	// Database for chat history storage
	chatDB database.Database

	// Polling system components
	eventStore      *events.EventStore
	observerManager *events.ObserverManager

	// Workflow orchestrator configuration
	provider      string
	model         string
	mcpConfigPath string
	temperature   float64
	workspaceRoot string
	eventBridge   interface{}

	// Active session tracking for page refresh recovery
	activeSessions    map[string]*ActiveSessionInfo
	activeSessionsMux sync.RWMutex
	internalLLM       llms.Model

	// Orchestrator objects in memory for guidance injection
	workflowOrchestrators map[string]*orchtypes.WorkflowOrchestrator
	plannerOrchestrators  map[string]*orchtypes.PlannerOrchestrator

	toolStatus    map[string]ToolStatus
	enabledTools  map[string][]string // queryID/sessionID -> enabled tool names
	toolStatusMux sync.RWMutex
	configPath    string
	mcpConfig     *mcpclient.MCPConfig

	// Background tool discovery
	discoveryRunning bool
	discoveryMux     sync.RWMutex
	lastDiscovery    time.Time
	discoveryTicker  *time.Ticker

	// Logger for structured logging
	logger utils.ExtendedLogger
}

// OrchestratorAgentEventBridge bridges individual agent events from within orchestrator to the main server event system
type OrchestratorAgentEventBridge struct {
	eventStore      *events.EventStore
	observerManager *events.ObserverManager
	observerID      string // Observer ID for polling API
	sessionID       string // Session ID for database storage
	logger          utils.ExtendedLogger
	agent           *mcpagent.Agent   // Add agent reference for hierarchy support
	chatDB          database.Database // Add database reference for chat history storage
}

// Name returns the bridge name
func (b *OrchestratorAgentEventBridge) Name() string {
	return "orchestrator_agent_event_bridge"
}

// HandleEvent processes agent events from within orchestrator and converts them to server events
func (b *OrchestratorAgentEventBridge) HandleEvent(ctx context.Context, event *unifiedevents.AgentEvent) error {
	b.logger.Infof("[ORCHESTRATOR AGENT BRIDGE] Processing agent event: %s", event.Type)

	// ✅ HIERARCHY FIX: For orchestrator events, use agent's EmitTypedEvent to get proper hierarchy
	if b.agent != nil && isOrchestratorEvent(event.Type) {
		b.logger.Infof("[ORCHESTRATOR AGENT BRIDGE] Using agent.EmitTypedEvent for orchestrator event: %s", event.Type)
		b.agent.EmitTypedEvent(ctx, event.Data)
		return nil
	}

	// For other events, use the original bridge logic
	// Create server event with typed AgentEvent data directly - no conversion needed!
	serverEvent := events.Event{
		ID:        fmt.Sprintf("orch_agent_%s_%d", event.Type, time.Now().UnixNano()),
		Type:      string(event.Type),
		Timestamp: time.Now(),
		Data:      event,        // Pass through the typed AgentEvent directly
		SessionID: b.observerID, // Use observerID for in-memory storage (polling)
	}

	// Store the event in the server's event store for polling API
	// Use the observer ID for in-memory storage (this is what the frontend polls)
	b.eventStore.AddEvent(b.observerID, serverEvent)

	// ✅ CHAT HISTORY FIX: Store event in database for chat history
	if b.chatDB != nil {
		// Extract hierarchy information from event data if available
		hierarchyLevel := 0
		component := "orchestrator"

		// Try to extract hierarchy info from BaseEventData if the event data has it
		if baseData, ok := event.Data.(interface {
			GetBaseEventData() *unifiedevents.BaseEventData
		}); ok {
			if base := baseData.GetBaseEventData(); base != nil {
				hierarchyLevel = base.HierarchyLevel
				if base.Component != "" {
					component = base.Component
				}
			}
		}

		// Convert unified event to database-compatible agent event
		// unifiedevents is just an alias for events package, so we can use it directly
		agentEvent := &unifiedevents.AgentEvent{
			Type:           event.Type,
			Timestamp:      event.Timestamp,
			EventIndex:     0, // Will be set by database
			TraceID:        event.TraceID,
			SpanID:         event.SpanID,
			ParentID:       event.ParentID,
			CorrelationID:  event.CorrelationID,
			Data:           event.Data,
			HierarchyLevel: hierarchyLevel, // Use extracted hierarchy level
			SessionID:      b.sessionID,    // Use sessionID for database storage
			Component:      component,      // Use extracted component
		}

		// Store in database using the session ID (same as chat session)
		// The orchestrator events don't set SessionID, so we always use the bridge's session ID
		b.logger.Infof("[ORCHESTRATOR AGENT BRIDGE] DEBUG: Using sessionID=%s for database storage (observerID=%s)", b.sessionID, b.observerID)
		if err := b.chatDB.StoreEvent(ctx, b.sessionID, agentEvent); err != nil {
			b.logger.Errorf("[ORCHESTRATOR AGENT BRIDGE] Failed to store event in database: %v", err)
		} else {
			b.logger.Infof("[ORCHESTRATOR AGENT BRIDGE] Stored event %s in database for chat history (hierarchy: %d, component: %s)", event.Type, hierarchyLevel, component)
		}
	}

	b.logger.Infof("[ORCHESTRATOR AGENT BRIDGE] Successfully bridged agent event: %s (typed data preserved)", event.Type)
	return nil
}

// isOrchestratorEvent checks if the event type is an orchestrator event
func isOrchestratorEvent(eventType unifiedevents.EventType) bool {
	return eventType == unifiedevents.OrchestratorStart ||
		eventType == unifiedevents.OrchestratorEnd ||
		eventType == unifiedevents.OrchestratorError ||
		eventType == unifiedevents.OrchestratorAgentStart ||
		eventType == unifiedevents.OrchestratorAgentEnd ||
		eventType == unifiedevents.OrchestratorAgentError
}

// getExecutionModeLabel returns a human-readable label for execution mode
func getExecutionModeLabel(mode string) string {
	return orchtypes.ParseExecutionMode(mode).GetLabel()
}

// QueryRequest represents an agent query request
type QueryRequest struct {
	Query          string               `json:"query"`
	Servers        []string             `json:"servers,omitempty"`
	EnabledServers []string             `json:"enabled_servers,omitempty"`
	Provider       string               `json:"provider,omitempty"`
	ModelID        string               `json:"model_id,omitempty"`
	Temperature    float64              `json:"temperature,omitempty"`
	MaxTurns       int                  `json:"max_turns,omitempty"`
	AgentMode      string               `json:"agent_mode,omitempty"`
	LLMConfig      *orchtypes.LLMConfig `json:"llm_config,omitempty"`
	PresetQueryID  string               `json:"preset_query_id,omitempty"`
	LLMGuidance    string               `json:"llm_guidance,omitempty"` // LLM guidance message
	// Orchestrator execution mode selection
	OrchestratorExecutionMode orchtypes.ExecutionMode `json:"orchestrator_execution_mode,omitempty"`
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// QueryResponse represents an agent query response
type QueryResponse struct {
	QueryID string `json:"query_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
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
	ServerCmd.Flags().String("provider", "bedrock", "LLM provider (bedrock, openai)")
	ServerCmd.Flags().String("model", "", "Model ID (uses provider default if empty)")
	ServerCmd.Flags().Float64("temperature", 0.2, "Temperature for LLM")
	ServerCmd.Flags().Int("max-turns", 30, "Maximum conversation turns")
	ServerCmd.Flags().String("mcp-config", "configs/mcp_servers_clean.json", "MCP servers configuration path")
	ServerCmd.Flags().String("agent-mode", "simple", "Agent mode (simple, react)")

	// Structured Output LLM flags
	ServerCmd.Flags().String("structured-output-provider", "", "Structured output LLM provider (uses main provider if empty)")
	ServerCmd.Flags().String("structured-output-model", "", "Structured output model ID (uses main model if empty)")
	ServerCmd.Flags().Float64("structured-output-temp", 0.0, "Structured output temperature (uses main temperature if 0)")

	// Chat History Database flags
	ServerCmd.Flags().String("db-path", "/app/chat_history.db", "SQLite database path for chat history")

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

	absConfigPath, err := filepath.Abs(config.MCPConfigPath)
	if err != nil {
		fmt.Printf("[DEBUG] Could not resolve absolute config path: %v\n", err)
	} else {
		fmt.Printf("[DEBUG] Absolute config path: %s\n", absConfigPath)
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
		log.Fatalf("Invalid provider: %v", err)
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
	mcpConfig, err := mcpclient.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load MCP config: %v", err)
	}

	// Initialize polling system
	eventStore := events.NewEventStore(10000) // Max 10000 events per observer
	observerManager := events.NewObserverManager(eventStore)

	// Initialize chat history database
	dbPath := viper.GetString("db-path")
	if dbPath == "" {
		dbPath = "/app/chat_history.db" // Default SQLite database path
	}

	chatDB, err := database.NewSQLiteDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize chat history database: %v", err)
	}
	defer chatDB.Close()

	fmt.Printf("💾 Chat History Database: %s\n", dbPath)

	// Create internal LLM instance for workflow orchestrator
	internalLLMProvider, err := llm.ValidateProvider(config.Provider)
	if err != nil {
		log.Fatalf("Invalid internal LLM provider: %v", err)
	}

	internalLLMConfig := llm.Config{
		Provider:    internalLLMProvider,
		ModelID:     config.ModelID,
		Temperature: config.Temperature,
		Logger:      createServerLogger(),
	}
	internalLLM, err := llm.InitializeLLM(internalLLMConfig)
	if err != nil {
		log.Fatalf("Failed to create internal LLM: %v", err)
	}

	api := &StreamingAPI{
		config:           config,
		agentCancelFuncs: make(map[string]context.CancelFunc),
		// orchestrators:                make(map[string]*orchtypes.PlannerOrchestrator), // Removed legacy
		orchestratorContexts:         make(map[string]context.CancelFunc),
		workflowOrchestratorContexts: make(map[string]context.CancelFunc),
		workflowObjectives:           make(map[string]string),
		conversationHistory:          make(map[string][]llms.MessageContent),
		orchestratorStates:           make(map[string]*StoredOrchestratorState),
		chatDB:                       chatDB,
		eventStore:                   eventStore,
		observerManager:              observerManager,
		provider:                     config.Provider,
		model:                        config.ModelID,
		mcpConfigPath:                configPath,
		temperature:                  config.Temperature,
		workspaceRoot:                "./Tasks",
		eventBridge:                  nil, // Will be set per request
		internalLLM:                  internalLLM,
		toolStatus:                   make(map[string]ToolStatus),
		enabledTools:                 make(map[string][]string),
		configPath:                   configPath,
		mcpConfig:                    mcpConfig,
		logger:                       createServerLogger(),
		// Initialize background discovery fields
		discoveryRunning: false,
		lastDiscovery:    time.Time{},
		discoveryTicker:  nil,
		// Initialize active session tracking
		activeSessions: make(map[string]*ActiveSessionInfo),
		// Initialize orchestrator storage
		workflowOrchestrators: make(map[string]*orchtypes.WorkflowOrchestrator),
		plannerOrchestrators:  make(map[string]*orchtypes.PlannerOrchestrator),
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

	// MCP Registry API routes (from mcp_registry_routes.go)
	apiRouter.HandleFunc("/mcp-registry/servers", api.handleGetMCPRegistryServers).Methods("GET")
	apiRouter.HandleFunc("/mcp-registry/servers/{id}", api.handleGetMCPRegistryServerDetails).Methods("GET")
	apiRouter.HandleFunc("/mcp-registry/servers/{id}/tools", api.handleGetMCPRegistryServerTools).Methods("POST")

	// MCP Config API routes (from mcp_config_routes.go)
	apiRouter.HandleFunc("/mcp-config", api.handleGetMCPConfig).Methods("GET")
	apiRouter.HandleFunc("/mcp-config", api.handleSaveMCPConfig).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/discover", api.handleDiscoverServers).Methods("POST")
	apiRouter.HandleFunc("/mcp-config/status", api.handleGetMCPConfigStatus).Methods("GET")

	// Polling API routes (from polling.go)
	apiRouter.HandleFunc("/observer/register", api.handleRegisterObserver).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/observer/{observer_id}/events", api.handleGetEvents).Methods("GET")
	apiRouter.HandleFunc("/observer/{observer_id}/status", api.handleGetObserverStatus).Methods("GET")
	apiRouter.HandleFunc("/observer/{observer_id}", api.handleRemoveObserver).Methods("DELETE")

	// Active Session API routes (from polling.go)
	apiRouter.HandleFunc("/sessions/active", api.handleGetActiveSessions).Methods("GET")
	apiRouter.HandleFunc("/sessions/{session_id}/reconnect", api.handleReconnectSession).Methods("POST")
	apiRouter.HandleFunc("/sessions/{session_id}/status", api.handleGetSessionStatus).Methods("GET")

	// LLM Guidance API routes
	apiRouter.HandleFunc("/sessions/{session_id}/llm-guidance", api.handleSetLLMGuidance).Methods("POST", "OPTIONS")

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

	// Workflow API routes
	apiRouter.HandleFunc("/workflow/create", api.handleCreateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/status", api.handleGetWorkflowStatus).Methods("GET")
	apiRouter.HandleFunc("/workflow/update", api.handleUpdateWorkflow).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/workflow/constants", orchtypes.HandleWorkflowConstants).Methods("GET")

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
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	fmt.Printf("✅ Server started on %s:%d\n", config.Host, config.Port)
	fmt.Printf("🔗 API endpoint: http://%s:%d/api/query\n", config.Host, config.Port)
	fmt.Printf("📡 Polling API: http://%s:%d/api/observer/{observer_id}/events\n", config.Host, config.Port)

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
		log.Fatalf("Server forced to shutdown: %v", err)
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
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Session-ID, X-Observer-ID")
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
		"providers":   []string{"bedrock", "openai"},
		"streaming":   true,
		"sse":         true,
		"agent_modes": []string{"simple", "react", "orchestrator", "workflow"},
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

// handleValidateAPIKey validates API keys for OpenRouter, OpenAI, and Bedrock
func (api *StreamingAPI) handleValidateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req llm.APIKeyValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode API key validation request: %v", err)
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
		errorMsg := fmt.Sprintf("Invalid request body: %v", err)
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
		"method":      r.Method,
		"url":         r.URL.String(),
		"user_agent":  r.Header.Get("User-Agent"),
		"session_id":  r.Header.Get("X-Session-ID"),
		"observer_id": r.Header.Get("X-Observer-ID"),
		"query":       req.Query,
		"query_id":    queryID,
	})

	// Set agent execution LLM defaults: API request takes precedence, then environment variables, then server config, then fallback to Bedrock
	agentProvider := req.Provider // API request takes highest priority
	log.Printf("[PROVIDER DEBUG] req.Provider: '%s'", req.Provider)
	if agentProvider == "" {
		agentProvider = os.Getenv("AGENT_PROVIDER") // Environment variable as fallback
		log.Printf("[PROVIDER DEBUG] AGENT_PROVIDER env var: '%s'", os.Getenv("AGENT_PROVIDER"))
	}
	if agentProvider == "" {
		agentProvider = api.config.Provider // Server config as fallback
		log.Printf("[PROVIDER DEBUG] api.config.Provider: '%s'", api.config.Provider)
	}
	if agentProvider == "" {
		agentProvider = "bedrock" // Default fallback
		log.Printf("[PROVIDER DEBUG] Using default fallback: 'bedrock'")
	}
	log.Printf("[PROVIDER DEBUG] Final agentProvider: '%s'", agentProvider)

	// Set agent model: API request takes precedence, then environment variables, then server config
	agentModel := req.ModelID // API request takes highest priority
	log.Printf("[MODEL DEBUG] req.ModelID: '%s'", req.ModelID)
	if agentModel == "" {
		agentModel = os.Getenv("AGENT_MODEL") // Environment variable as fallback
		log.Printf("[MODEL DEBUG] AGENT_MODEL env var: '%s'", os.Getenv("AGENT_MODEL"))
	}
	if agentModel == "" {
		agentModel = api.config.ModelID // Server config as fallback
		log.Printf("[MODEL DEBUG] api.config.ModelID: '%s'", api.config.ModelID)
	}
	if agentModel == "" && agentProvider == "bedrock" {
		agentModel = os.Getenv("BEDROCK_PRIMARY_MODEL") // Use .env configuration
		log.Printf("[MODEL DEBUG] BEDROCK_PRIMARY_MODEL env var: '%s'", os.Getenv("BEDROCK_PRIMARY_MODEL"))
	}
	log.Printf("[MODEL DEBUG] Final agentModel: '%s'", agentModel)
	req.Provider = agentProvider
	req.ModelID = agentModel

	// Use enabled_servers if provided, otherwise fall back to servers
	selectedServers := req.EnabledServers
	if len(selectedServers) == 0 {
		selectedServers = req.Servers
	}

	// Default to all servers if none specified
	if len(selectedServers) == 0 {
		selectedServers = []string{"all"}
	}

	// Convert server array to comma-separated string for agent compatibility
	serverList := strings.Join(selectedServers, ",")

	// Debug logging for server selection
	log.Printf("[SERVER DEBUG] Request enabled_servers: %v", req.EnabledServers)
	log.Printf("[SERVER DEBUG] Request servers: %v", req.Servers)
	log.Printf("[SERVER DEBUG] Selected servers: %v", selectedServers)
	log.Printf("[SERVER DEBUG] Server list: %s", serverList)

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
		log.Printf("[DATABASE DEBUG] Creating new chat session for sessionID: %s", sessionID)
		// Truncate query for title
		title := req.Query
		log.Printf("[TITLE DEBUG] Query received for title: '%s' (length: %d)", title, len(title))
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		log.Printf("[TITLE DEBUG] Final title: '%s'", title)
		chatSession, err = api.chatDB.CreateChatSession(r.Context(), &database.CreateChatSessionRequest{
			SessionID: sessionID,
			Title:     title,
			AgentMode: req.AgentMode,
		})
		if err != nil {
			log.Printf("[DATABASE DEBUG] Failed to create chat session: %v", err)
			// Continue without chat session - events won't be stored but query can proceed
		} else {
			log.Printf("[DATABASE DEBUG] Successfully created chat session: %s", chatSession.ID)
		}
	} else {
		log.Printf("[DATABASE DEBUG] Found existing chat session: %s", chatSession.ID)
	}

	// Extract observer ID from request
	observerID := r.Header.Get("X-Observer-ID")

	// Create observer if not provided
	if observerID == "" {
		observer := api.observerManager.RegisterObserver(sessionID)
		observerID = observer.ID
		log.Printf("[ACTIVE_SESSION] Created observer %s for session %s", observerID, sessionID)
	}

	// Track active session for page refresh recovery
	api.trackActiveSession(sessionID, observerID, req.AgentMode, req.Query)

	// Create a fresh agent for each request
	log.Printf("[LLM CONFIG DEBUG] Creating fresh agent for each request")

	// Return immediate response with query ID
	response := QueryResponse{
		QueryID: queryID,
		Status:  "started",
		Message: "Query processing started. Use polling API to get real-time updates.",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
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
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error: %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status", sessionID)
					}
				}

				// Emit server-level error completion event
				if observerID != "" {
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
					agentEvent.SessionID = observerID

					serverErrorEvent := events.Event{
						ID:        fmt.Sprintf("server_error_%s_%d", queryID, time.Now().UnixNano()),
						Type:      string(unifiedevents.EventTypeUnifiedCompletion),
						Timestamp: time.Now(),
						Data:      agentEvent,
						SessionID: observerID,
					}
					api.eventStore.AddEvent(observerID, serverErrorEvent)
					log.Printf("[SERVER DEBUG] Emitted server error completion event for query %s", queryID)
				}
			}
		}

		// Validate provider
		llmProvider, err := llm.ValidateProvider(req.Provider)
		if err != nil {
			sendError(fmt.Sprintf("Invalid provider: %v", err), true)
			return
		}

		// Validate LLM provider - no need to initialize since agent wrapper handles it
		_ = llmProvider // Use provider variable to avoid unused variable error

		// Create context with timeout for the entire streaming operation
		streamCtx, cancel := context.WithTimeout(context.Background(), 60*3*time.Minute)
		defer cancel()

		// Handle orchestrator mode first to avoid unnecessary agent creation
		if req.AgentMode == "orchestrator" {
			// Check if there's stored orchestrator state for this session
			storedState, hasStoredState := api.getOrchestratorState(sessionID)

			if hasStoredState {
				log.Printf("[ORCHESTRATOR DEBUG] Found stored planner orchestrator state for session %s - will restore and continue", sessionID)
				log.Printf("[ORCHESTRATOR DEBUG] Stored state: iteration %d, step %d, phase %s",
					storedState.CurrentIteration, storedState.CurrentStepIndex, storedState.CurrentPhase)
			} else {
				log.Printf("[ORCHESTRATOR DEBUG] No stored planner orchestrator state for session %s - starting fresh", sessionID)
			}

			log.Printf("[ORCHESTRATOR DEBUG] Orchestrator mode requested for query %s", queryID)
			log.Printf("[ORCHESTRATOR DEBUG] Cache-only mode: true (always enabled)")

			// Create observer for orchestrator polling API
			if observerID == "" {
				observer := api.observerManager.RegisterObserver(sessionID)
				observerID = observer.ID
				log.Printf("[ORCHESTRATOR DEBUG] Created observer %s for orchestrator session %s", observerID, sessionID)
			}

			// Update chat session for orchestrator (it may already exist from regular flow)
			if api.chatDB != nil {
				log.Printf("[ORCHESTRATOR DEBUG] Updating chat session for orchestrator session %s", sessionID)

				// Get existing chat session to preserve preset_query_id
				existingSession, err := api.chatDB.GetChatSession(streamCtx, sessionID)
				var presetQueryID string
				if err != nil {
					log.Printf("[ORCHESTRATOR DEBUG] Could not get existing chat session: %v", err)
					presetQueryID = "" // No preset if session doesn't exist
				} else {
					if existingSession.PresetQueryID != nil {
						presetQueryID = *existingSession.PresetQueryID
						log.Printf("[ORCHESTRATOR DEBUG] Found existing preset_query_id: %s", presetQueryID)
					} else {
						presetQueryID = ""
						log.Printf("[ORCHESTRATOR DEBUG] No preset_query_id in existing session")
					}
				}

				updateReq := &database.UpdateChatSessionRequest{
					Title:         req.Query,
					AgentMode:     "orchestrator",
					PresetQueryID: presetQueryID,
				}
				_, err = api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
				if err != nil {
					log.Printf("[ORCHESTRATOR ERROR] Failed to update chat session: %v", err)
				} else {
					log.Printf("[ORCHESTRATOR DEBUG] Updated chat session with orchestrator title, mode, and preset_query_id: %s", presetQueryID)
				}
			}

			// Create a bridge to connect individual agent events from within orchestrator to the main server event system
			orchestratorAgentEventBridge := &OrchestratorAgentEventBridge{
				eventStore:      api.eventStore,
				observerManager: api.observerManager,
				observerID:      observerID, // Use observerID for polling API
				sessionID:       sessionID,  // Use sessionID for database storage
				logger:          api.logger,
				agent:           nil,        // No agent reference needed for orchestrator mode
				chatDB:          api.chatDB, // Add database reference for event storage
			}

			// Create selected options for orchestrator execution mode
			var selectedOptions *orchtypes.PlannerSelectedOptions
			if req.OrchestratorExecutionMode != "" {
				// Create selected options with execution mode
				selectedOptions = &orchtypes.PlannerSelectedOptions{
					Selections: []orchtypes.PlannerSelectedOption{
						{
							OptionID:    req.OrchestratorExecutionMode.String(),
							OptionLabel: req.OrchestratorExecutionMode.GetLabel(),
							OptionValue: req.OrchestratorExecutionMode.String(),
							Group:       "execution_strategy",
						},
					},
				}
				log.Printf("[ORCHESTRATOR DEBUG] Using execution mode from request: %s", req.OrchestratorExecutionMode.String())
			} else {
				// Default to sequential execution if no mode specified
				defaultMode := orchtypes.SequentialExecution
				selectedOptions = &orchtypes.PlannerSelectedOptions{
					Selections: []orchtypes.PlannerSelectedOption{
						{
							OptionID:    defaultMode.String(),
							OptionLabel: defaultMode.GetLabel(),
							OptionValue: defaultMode.String(),
							Group:       "execution_strategy",
						},
					},
				}
				log.Printf("[ORCHESTRATOR DEBUG] Using default execution mode: %s", defaultMode.String())
			}

			// Always create a fresh orchestrator for this session
			orchestrator := orchtypes.NewPlannerOrchestrator(
				api.logger,
				api.config.AgentMode,
				selectedOptions, // Pass selected options with execution mode
			)
			log.Printf("[ORCHESTRATOR DEBUG] Created fresh orchestrator for session %s", sessionID)

			// Initialize orchestrator agents
			// Use server's default temperature if request doesn't provide one
			temperature := req.Temperature
			if temperature == 0.0 {
				temperature = api.config.Temperature
				log.Printf("[ORCHESTRATOR DEBUG] Using server default temperature: %.2f", temperature)
			}

			// Convert frontend LLM config to orchestrator format
			var llmConfig *orchtypes.LLMConfig
			var orchestratorProvider string
			var orchestratorModel string

			if req.LLMConfig != nil {
				llmConfig = &orchtypes.LLMConfig{
					Provider:       req.LLMConfig.Provider,
					ModelID:        req.LLMConfig.ModelID,
					FallbackModels: req.LLMConfig.FallbackModels,
				}

				// Only set cross-provider fallback if it's not nil
				if req.LLMConfig.CrossProviderFallback != nil {
					llmConfig.CrossProviderFallback = &agents.CrossProviderFallback{
						Provider: req.LLMConfig.CrossProviderFallback.Provider,
						Models:   req.LLMConfig.CrossProviderFallback.Models,
					}
				}
				// Use LLM config values for orchestrator initialization
				orchestratorProvider = req.LLMConfig.Provider
				orchestratorModel = req.LLMConfig.ModelID
				log.Printf("[ORCHESTRATOR LLM CONFIG DEBUG] Using detailed LLM config - Provider: %s, Model: %s, Fallbacks: %v, CrossProvider: %+v",
					llmConfig.Provider, llmConfig.ModelID, llmConfig.FallbackModels, llmConfig.CrossProviderFallback)
			} else {
				// Fall back to request defaults
				orchestratorProvider = req.Provider
				orchestratorModel = req.ModelID
				log.Printf("[ORCHESTRATOR LLM CONFIG DEBUG] Using basic config - Provider: %s, Model: %s", req.Provider, req.ModelID)
			}

			// Create custom tools for orchestrator agents (workspace tools + human tools)
			// Orchestrator agents can be Simple or ReAct agents, tools are registered based on mode
			// TODO: Memory tools removed from orchestrator - only needed for individual React agents
			// memoryTools := virtualtools.CreateMemoryTools()
			// memoryExecutors := virtualtools.CreateMemoryToolExecutors()
			workspaceTools := virtualtools.CreateWorkspaceTools()
			workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
			humanTools := virtualtools.CreateHumanTools()
			humanExecutors := virtualtools.CreateHumanToolExecutors()

			// Combine workspace and human tools for orchestrator agents
			allTools := append(workspaceTools, humanTools...)
			allExecutors := make(map[string]interface{})
			for name, executor := range workspaceExecutors {
				allExecutors[name] = executor
			}
			for name, executor := range humanExecutors {
				allExecutors[name] = executor
			}

			// Always initialize fresh orchestrator with custom tools
			err := orchestrator.InitializeAgents(
				streamCtx,
				orchestratorProvider, // Use the correct provider (from LLM config or fallback)
				orchestratorModel,    // Use the correct model (from LLM config or fallback)
				api.configPath,
				traceID,
				temperature,
				orchestratorAgentEventBridge, // Pass the agent event bridge
				selectedServers,              // Pass the selected servers from frontend
				false,                        // Disable cache-only mode to allow fresh connections
				llmConfig,                    // Pass detailed LLM configuration
				tracer,                       // Pass the Langfuse tracer
				api.logger,                   // Pass the logger
				allTools,                     // Pass all custom tools (workspace + human)
				allExecutors,                 // Pass all custom tool executors
			)
			if err != nil {
				log.Printf("[ORCHESTRATOR ERROR] Failed to initialize orchestrator agents: %v", err)
			} else {
				log.Printf("[ORCHESTRATOR DEBUG] Successfully initialized fresh orchestrator agents")
			}

			// Custom tools are now passed during InitializeAgents, so no need for separate SetCustomTools call
			log.Printf("[ORCHESTRATOR DEBUG] Custom tools (%d workspace + %d human = %d total) passed during InitializeAgents", len(workspaceTools), len(humanTools), len(allTools))

			// Restore state if available
			if hasStoredState && err == nil {
				log.Printf("[ORCHESTRATOR DEBUG] Restoring orchestrator state for session %s", sessionID)
				if restoreErr := orchestrator.RestoreState(storedState); restoreErr != nil {
					log.Printf("[ORCHESTRATOR ERROR] Failed to restore orchestrator state: %v", restoreErr)
					// Continue without restored state - will start fresh
				} else {
					log.Printf("[ORCHESTRATOR DEBUG] Successfully restored orchestrator state: iteration %d, step %d, phase %s",
						storedState.CurrentIteration, storedState.CurrentStepIndex, storedState.CurrentPhase)
				}
			}

			if err != nil {

				// Emit orchestrator error event for frontend visibility
				if observerID != "" {
					orchestratorErrorEvent := &unifiedevents.OrchestratorErrorEvent{
						BaseEventData: unifiedevents.BaseEventData{
							Timestamp: time.Now(),
						},
						Context:  "orchestrator_initialization",
						Error:    err.Error(),
						Duration: time.Since(startTime),
					}

					// Create unified event wrapper
					unifiedEvent := &unifiedevents.AgentEvent{
						Type:      unifiedevents.OrchestratorError,
						Timestamp: time.Now(),
						Data:      orchestratorErrorEvent,
					}

					// Emit through the event store
					serverErrorEvent := events.Event{
						ID:        fmt.Sprintf("orchestrator_error_%s_%d", queryID, time.Now().UnixNano()),
						Type:      string(unifiedevents.OrchestratorError),
						Timestamp: time.Now(),
						Data:      unifiedEvent,
						SessionID: observerID,
					}
					api.eventStore.AddEvent(observerID, serverErrorEvent)
					log.Printf("[SERVER DEBUG] Emitted orchestrator error event for query %s", queryID)
				}

				sendError(fmt.Sprintf("Failed to initialize orchestrator: %v", err), true)
				return
			}

			// Store planner orchestrator for guidance injection
			api.storePlannerOrchestrator(sessionID, orchestrator)

			// Load conversation history for orchestrator
			api.conversationMux.RLock()
			history, exists := api.conversationHistory[sessionID]
			api.conversationMux.RUnlock()

			if exists && len(history) > 0 {
				log.Printf("[ORCHESTRATOR DEBUG] Loading %d messages from conversation history for orchestrator session %s", len(history), sessionID)
			} else {
				log.Printf("[ORCHESTRATOR DEBUG] No conversation history found for orchestrator session %s, starting fresh", sessionID)
			}

			// Create a cancellable context for orchestrator execution using background context
			// This prevents the orchestrator from being cancelled when the HTTP request ends
			orchestratorCtx, orchestratorCancel := context.WithCancel(context.Background())

			// Store the cancel function for potential cancellation
			api.orchestratorContextMux.Lock()
			api.orchestratorContexts[sessionID] = orchestratorCancel
			api.orchestratorContextMux.Unlock()

			// Execute orchestrator flow asynchronously to support streaming and cancellation
			go func() {
				defer func() {
					// Clean up the cancel function when done
					api.orchestratorContextMux.Lock()
					delete(api.orchestratorContexts, sessionID)
					api.orchestratorContextMux.Unlock()
				}()

				log.Printf("[ORCHESTRATOR DEBUG] Starting asynchronous orchestrator execution for query %s", queryID)

				// Emit orchestrator start event
				if observerID != "" {
					orchestratorStartEvent := &unifiedevents.OrchestratorStartEvent{
						BaseEventData: unifiedevents.BaseEventData{
							Timestamp: time.Now(),
						},
						Objective:     req.Query,
						AgentsCount:   5, // planning, execution, validation, organizer, report generation
						ServersCount:  len(selectedServers),
						Configuration: fmt.Sprintf("Provider: %s, Model: %s", req.Provider, req.ModelID),
						ExecutionMode: string(req.OrchestratorExecutionMode), // Use the execution mode from the request
					}

					// Create unified event wrapper
					unifiedEvent := &unifiedevents.AgentEvent{
						Type:      unifiedevents.OrchestratorStart,
						Timestamp: time.Now(),
						Data:      orchestratorStartEvent,
					}

					// Emit through the bridge
					orchestratorAgentEventBridge.HandleEvent(orchestratorCtx, unifiedEvent)
					log.Printf("[ORCHESTRATOR DEBUG] Emitted orchestrator start event")
				}

				// Execute orchestrator flow with conversation history using cancellable context
				// The orchestrator will automatically continue from restored state if available
				log.Printf("[ORCHESTRATOR DEBUG] Starting orchestrator execution for query %s", queryID)
				result, err := orchestrator.ExecuteFlow(orchestratorCtx, req.Query, history, orchestratorAgentEventBridge)

				// Check for orchestrator execution error
				if err != nil {
					log.Printf("[ORCHESTRATOR ERROR] Orchestrator execution failed: %v", err)

					// Update chat session status to error
					if api.chatDB != nil {
						updateReq := &database.UpdateChatSessionRequest{
							Status: "error",
						}
						_, updateErr := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
						if updateErr != nil {
							log.Printf("[ORCHESTRATOR ERROR] Failed to update chat session status to error: %v", updateErr)
						} else {
							log.Printf("[ORCHESTRATOR DEBUG] Updated chat session %s to error status", sessionID)
						}
					}

					// Update active session status to error
					api.updateSessionStatus(sessionID, "error")

					// Send error response
					sendError(fmt.Sprintf("Orchestrator execution failed: %v", err), true)
					return
				}

				// Build response from orchestrator result
				orchestratorResponse := "🎭 **Orchestrator Mode - Multi-Agent Execution**\n\n" +
					"**Query:** " + req.Query + "\n\n" +
					"**Result:**\n" + result

				// Log result length for debugging
				log.Printf("[ORCHESTRATOR DEBUG] Raw orchestrator result length: %d characters", len(result))
				log.Printf("[ORCHESTRATOR DEBUG] Full response length: %d characters", len(orchestratorResponse))

				// Save orchestrator result to conversation history
				assistantText := strings.TrimSpace(orchestratorResponse)
				if assistantText != "" {
					// Create assistant message for conversation history
					assistantMessage := llms.MessageContent{
						Role:  llms.ChatMessageTypeAI,
						Parts: []llms.ContentPart{llms.TextContent{Text: assistantText}},
					}

					// Add user message
					userMessage := llms.MessageContent{
						Role:  llms.ChatMessageTypeHuman,
						Parts: []llms.ContentPart{llms.TextContent{Text: req.Query}},
					}

					// Update conversation history
					api.conversationMux.Lock()
					if existingHistory, exists := api.conversationHistory[sessionID]; exists {
						// Append to existing history
						api.conversationHistory[sessionID] = append(existingHistory, userMessage, assistantMessage)
					} else {
						// Create new history
						api.conversationHistory[sessionID] = []llms.MessageContent{userMessage, assistantMessage}
					}
					api.conversationMux.Unlock()

					log.Printf("[ORCHESTRATOR DEBUG] Saved orchestrator result to conversation history for session %s", sessionID)
				}

				// Update chat session status to completed
				if api.chatDB != nil {
					updateReq := &database.UpdateChatSessionRequest{
						Status: "completed",
					}
					_, updateErr := api.chatDB.UpdateChatSession(streamCtx, sessionID, updateReq)
					if updateErr != nil {
						log.Printf("[ORCHESTRATOR ERROR] Failed to update chat session status to completed: %v", updateErr)
					} else {
						log.Printf("[ORCHESTRATOR DEBUG] Updated chat session %s to completed status", sessionID)
					}
				}

				// Update active session status to completed
				log.Printf("[COMPLETION] Updating session %s status to completed", sessionID)
				api.updateSessionStatus(sessionID, "completed")

				// End trace
				tracer.EndTrace(traceID, map[string]interface{}{
					"status": "completed",
				})

				// Emit orchestrator end event
				if observerID != "" {
					duration := time.Since(startTime)
					status := "completed"
					errorMsg := ""

					orchestratorEndEvent := &unifiedevents.OrchestratorEndEvent{
						BaseEventData: unifiedevents.BaseEventData{
							Timestamp: time.Now(),
						},
						Objective:     req.Query,
						Result:        result,
						Duration:      duration,
						Status:        status,
						Error:         errorMsg,
						ExecutionMode: string(req.OrchestratorExecutionMode), // Use the execution mode from the request
					}

					// Create unified event wrapper
					unifiedEvent := &unifiedevents.AgentEvent{
						Type:      unifiedevents.OrchestratorEnd,
						Timestamp: time.Now(),
						Data:      orchestratorEndEvent,
					}

					// Emit through the bridge
					orchestratorAgentEventBridge.HandleEvent(orchestratorCtx, unifiedEvent)
					log.Printf("[ORCHESTRATOR DEBUG] Emitted orchestrator end event")
				}

				// Emit server-level completion event for orchestrator mode
				if observerID != "" {
					completionEventData := unifiedevents.NewUnifiedCompletionEvent(
						"orchestrator",        // agentType
						"orchestrator",        // agentMode
						req.Query,             // question
						result,                // finalResult
						"completed",           // status
						time.Since(startTime), // duration
						1,                     // turns
					)

					// Log the completion event data length for debugging
					log.Printf("[ORCHESTRATOR DEBUG] Unified completion event final_result length: %d characters", len(result))

					agentEvent := unifiedevents.NewAgentEvent(completionEventData)
					agentEvent.SessionID = observerID

					serverCompletionEvent := events.Event{
						ID:        fmt.Sprintf("server_completion_%s_%d", queryID, time.Now().UnixNano()),
						Type:      string(unifiedevents.EventTypeUnifiedCompletion),
						Timestamp: time.Now(),
						Data:      agentEvent,
						SessionID: observerID,
					}
					api.eventStore.AddEvent(observerID, serverCompletionEvent)
					log.Printf("[SERVER DEBUG] Emitted orchestrator server completion event for query %s", queryID)
				}

				log.Printf("[ORCHESTRATOR DEBUG] Asynchronous orchestrator execution completed for query %s", queryID)
			}()

			return
		}

		// Create fresh agent for this request
		// Use LLM configuration from request if provided, otherwise use request defaults
		var finalProvider string
		var finalModelID string
		var fallbackModels []string
		var crossProviderFallback *agent.CrossProviderFallback

		if req.LLMConfig != nil {
			// Use LLM configuration from frontend
			finalProvider = req.LLMConfig.Provider
			finalModelID = req.LLMConfig.ModelID
			fallbackModels = req.LLMConfig.FallbackModels

			// Only set cross-provider fallback if it's not nil
			if req.LLMConfig.CrossProviderFallback != nil {
				crossProviderFallback = &agent.CrossProviderFallback{
					Provider: req.LLMConfig.CrossProviderFallback.Provider,
					Models:   req.LLMConfig.CrossProviderFallback.Models,
				}
			}
			log.Printf("[LLM CONFIG DEBUG] Using detailed LLM config from request - Provider: %s, Model: %s, Fallbacks: %v, CrossProvider: %+v",
				finalProvider, finalModelID, fallbackModels, crossProviderFallback)
		} else {
			// Fall back to request defaults
			finalProvider = req.Provider
			finalModelID = req.ModelID
			log.Printf("[LLM CONFIG DEBUG] Using request defaults - Provider: %s, Model: %s", finalProvider, finalModelID)
		}

		// Handle workflow mode - use workflow orchestrator
		if req.AgentMode == "workflow" {
			// Check if there's stored workflow state for this session
			storedWorkflowState, hasStoredWorkflowState := api.getWorkflowState(sessionID)

			if hasStoredWorkflowState {
				log.Printf("[WORKFLOW DEBUG] Found stored workflow state for session %s - will restore and continue", sessionID)
				log.Printf("[WORKFLOW DEBUG] Stored state: phase %s, todo index %d, step %d, cycle %d",
					storedWorkflowState.CurrentPhase, storedWorkflowState.CurrentTodoIndex, storedWorkflowState.CurrentStep, storedWorkflowState.ExecutionCycle)
			} else {
				log.Printf("[WORKFLOW DEBUG] No stored workflow state for session %s - starting fresh", sessionID)
			}

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

			log.Printf("[WORKFLOW DEBUG] Workflow mode requested for query %s - using workflow orchestrator", queryID)
			log.Printf("[WORKFLOW DEBUG] Current observerID: '%s', sessionID: '%s'", observerID, sessionID)
			log.Printf("[WORKFLOW DEBUG] Selected servers for workflow: %v", selectedServers)

			// Create observer for workflow polling API
			if observerID == "" {
				observer := api.observerManager.RegisterObserver(sessionID)
				observerID = observer.ID
				log.Printf("[WORKFLOW DEBUG] Created observer %s for workflow session %s", observerID, sessionID)
			} else {
				log.Printf("[WORKFLOW DEBUG] Using existing observer %s for workflow session %s", observerID, sessionID)
			}

			// Create workflow event bridge for event emission
			workflowEventBridge := &WorkflowEventBridge{
				eventStore:      api.eventStore,
				observerManager: api.observerManager,
				observerID:      observerID,
				sessionID:       sessionID,
				logger:          api.logger,
				chatDB:          api.chatDB,
			}

			// Create custom tools for workflow agents (workspace tools + human tools)
			// Workflow agents can be Simple or ReAct agents, tools are registered based on mode
			// TODO: Memory tools removed from workflow - only needed for individual React agents
			// memoryTools := virtualtools.CreateMemoryTools()
			// memoryExecutors := virtualtools.CreateMemoryToolExecutors()
			workspaceTools := virtualtools.CreateWorkspaceTools()
			workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
			humanTools := virtualtools.CreateHumanTools()
			humanExecutors := virtualtools.CreateHumanToolExecutors()

			// Combine workspace and human tools for workflow agents
			allTools := append(workspaceTools, humanTools...)
			allExecutors := make(map[string]interface{})
			for name, executor := range workspaceExecutors {
				allExecutors[name] = executor
			}
			for name, executor := range humanExecutors {
				allExecutors[name] = executor
			}

			// Create workflow orchestrator for this request
			workflowOrchestrator, err := orchtypes.NewWorkflowOrchestrator(
				streamCtx, // Pass the stream context
				finalProvider,
				finalModelID,
				api.mcpConfigPath,
				api.temperature,
				"workflow",
				api.workspaceRoot,
				api.logger,
				api.internalLLM,
				workflowEventBridge, // Use workflow event bridge for event emission
				tracer,              // Pass the tracer
				selectedServers,     // Pass the selected servers from frontend
			)
			if err != nil {
				log.Printf("[WORKFLOW ERROR] Failed to create workflow orchestrator: %v", err)
				http.Error(w, fmt.Sprintf("Failed to create workflow orchestrator: %v", err), http.StatusInternalServerError)
				return
			}

			// Initialize workflow orchestrator with custom tools
			err = workflowOrchestrator.InitializeAgents(streamCtx, allTools, allExecutors)
			if err != nil {
				log.Printf("[WORKFLOW ERROR] Failed to initialize workflow orchestrator: %v", err)
				http.Error(w, fmt.Sprintf("Failed to initialize workflow orchestrator: %v", err), http.StatusInternalServerError)
				return
			}
			log.Printf("[WORKFLOW DEBUG] Initialized workflow orchestrator with %d custom tools (%d workspace + %d human)", len(allTools), len(workspaceTools), len(humanTools))

			// Store workflow orchestrator for guidance injection
			api.storeWorkflowOrchestrator(sessionID, workflowOrchestrator)

			// Restore workflow state if available
			if hasStoredWorkflowState && storedWorkflowState != nil {
				log.Printf("[WORKFLOW DEBUG] Restoring workflow state for session %s", sessionID)
				if restoreErr := workflowOrchestrator.RestoreState(storedWorkflowState); restoreErr != nil {
					log.Printf("[WORKFLOW ERROR] Failed to restore workflow state: %v", restoreErr)
					// Continue without restored state - will start fresh
				} else {
					log.Printf("[WORKFLOW DEBUG] Successfully restored workflow state: phase %s, todo index %d, step %d, cycle %d",
						storedWorkflowState.CurrentPhase, storedWorkflowState.CurrentTodoIndex, storedWorkflowState.CurrentStep, storedWorkflowState.ExecutionCycle)
				}
			}

			// Create a cancellable context for workflow execution using background context
			// This prevents the workflow from being cancelled when the HTTP request ends
			workflowCtx, workflowCancel := context.WithCancel(context.Background())

			// Add debug logging for context creation
			log.Printf("[WORKFLOW DEBUG] Created workflow context: %p, parent: %p", workflowCtx, context.Background())
			log.Printf("[WORKFLOW DEBUG] Context error check: %v", workflowCtx.Err())

			// Store the cancel function for potential cancellation
			api.orchestratorContextMux.Lock()
			api.orchestratorContexts[sessionID] = workflowCancel
			api.orchestratorContextMux.Unlock()

			// Execute workflow asynchronously
			go func() {
				defer func() {
					// Clean up the cancel function when done
					api.orchestratorContextMux.Lock()
					delete(api.orchestratorContexts, sessionID)
					api.orchestratorContextMux.Unlock()

					// Note: Observer cleanup is handled by session management
					// Don't remove observer immediately to allow frontend polling
					log.Printf("[WORKFLOW DEBUG] Workflow completed, observer %s will be cleaned up by session management", observerID)
				}()

				log.Printf("[WORKFLOW DEBUG] Starting asynchronous workflow execution for query %s", queryID)

				// Add debug logging for context before execution
				log.Printf("[WORKFLOW DEBUG] Context before execution: %p, error: %v", workflowCtx, workflowCtx.Err())
				deadline, hasDeadline := workflowCtx.Deadline()
				log.Printf("[WORKFLOW DEBUG] Context deadline: %v, hasDeadline: %v", deadline, hasDeadline)
				log.Printf("[WORKFLOW DEBUG] Context done: %v", workflowCtx.Done())

				// Check database for workflow approval status if preset_id is provided
				workflowStatus := database.WorkflowStatusPreVerification // Default status
				var selectedOptions *database.WorkflowSelectedOptions
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
						} else {
							log.Printf("[WORKFLOW CHECK] No selected options found")
						}
					} else {
						log.Printf("[WORKFLOW CHECK] Could not check database: %v", err)
					}
				}

				log.Printf("[WORKFLOW EXECUTION] Executing workflow with status: %s", workflowStatus)

				// Execute workflow with the query
				_, err := workflowOrchestrator.ExecuteWorkflow(
					workflowCtx,
					queryID, // Use queryID as workflow ID
					req.Query,
					workflowStatus,  // Current workflow status
					selectedOptions, // Pass selected options from database
				)
				if err != nil {
					log.Printf("[WORKFLOW ERROR] Workflow execution failed for query %s: %v", queryID, err)
					// Send error event
					errorData := map[string]interface{}{
						"error":    err.Error(),
						"query_id": queryID,
					}
					api.eventStore.AddEvent(observerID, events.Event{
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
						SessionID: observerID,
					})
				} else {
					log.Printf("[WORKFLOW DEBUG] Workflow execution completed for query %s", queryID)
				}
			}()

			// Return immediately with observer info
			response := map[string]interface{}{
				"query_id":    queryID,
				"observer_id": observerID,
				"session_id":  sessionID,
				"status":      "workflow_started",
				"message":     "Workflow execution started",
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Create new agent with streamCtx instead of r.Context()
		agentConfig := agent.LLMAgentConfig{
			Name:               sessionID,
			ServerName:         serverList, // Use full server list, not just first one
			ConfigPath:         api.configPath,
			Provider:           llm.Provider(finalProvider),
			ModelID:            finalModelID,
			Temperature:        req.Temperature,
			MaxTurns:           req.MaxTurns,
			ToolChoice:         "auto",
			StreamingChunkSize: 50,
			Timeout:            2 * time.Minute,
			CacheOnly:          false, // Allow fresh connections when cache is not available

			// Enable smart routing by default for both React and Simple agents
			EnableSmartRouting:     true,
			SmartRoutingMaxTools:   20, // Enable when more than 20 tools
			SmartRoutingMaxServers: 4,  // Enable when more than 4 servers

			// Detailed LLM configuration from frontend
			FallbackModels:        fallbackModels,
			CrossProviderFallback: crossProviderFallback,
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
			agentConfig.AgentMode = mcpagent.ReActAgent // Default to ReAct mode
		}
		log.Printf("[AGENT DEBUG] Creating agent with mode: %s, servers: %s", agentConfig.AgentMode, serverList)
		log.Printf("[SMART ROUTING DEBUG] Smart routing enabled - MaxTools: %d, MaxServers: %d (using defaults for temperature/tokens)",
			agentConfig.SmartRoutingMaxTools, agentConfig.SmartRoutingMaxServers)
		log.Printf("[CACHE DEBUG] Cache-only mode: %v (disabled to allow fresh connections)", agentConfig.CacheOnly)
		// Create LLM agent wrapper with trace using streamCtx
		llmAgent, err := agent.NewLLMAgentWrapperWithTrace(streamCtx, agentConfig, tracer, traceID, api.logger)
		if err != nil {
			log.Printf("[AGENT DEBUG] Failed to create LLM agent wrapper: %v", err)
			sendError(fmt.Sprintf("Failed to create agent: %v", err), true)
			return
		}

		// Add custom agent instructions based on agent mode
		if underlyingAgent := llmAgent.GetUnderlyingAgent(); underlyingAgent != nil {
			// Add base instructions for all agents
			underlyingAgent.AppendSystemPrompt(GetAgentInstructions())

			// Add React-specific instructions and virtual tools only for React agents
			if agentConfig.AgentMode == mcpagent.ReActAgent {
				underlyingAgent.AppendSystemPrompt(GetReactAgentInstructions())

				// Register memory tools for React agents (commented out - memory API not running)
				// memoryTools := virtualtools.CreateMemoryTools()
				// memoryExecutors := virtualtools.CreateMemoryToolExecutors()

				// for _, tool := range memoryTools {
				// 	if executor, exists := memoryExecutors[tool.Function.Name]; exists {
				// 		// Type assert parameters to map[string]interface{}
				// 		params, ok := tool.Function.Parameters.(map[string]interface{})
				// 		if !ok {
				// 			log.Printf("[MEMORY TOOLS] Warning: Failed to convert parameters for tool %s", tool.Function.Name)
				// 			continue
				// 		}

				// 		underlyingAgent.RegisterCustomTool(
				// 			tool.Function.Name,
				// 			tool.Function.Description,
				// 			params,
				// 			executor,
				// 		)
				// 	}
				// }

				// Register workspace and human tools for all agents (both Simple and ReAct)
				workspaceTools := virtualtools.CreateWorkspaceTools()
				workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
				humanTools := virtualtools.CreateHumanTools()
				humanExecutors := virtualtools.CreateHumanToolExecutors()

				// Register workspace tools
				for _, tool := range workspaceTools {
					if executor, exists := workspaceExecutors[tool.Function.Name]; exists {
						// Type assert parameters to map[string]interface{}
						params, ok := tool.Function.Parameters.(map[string]interface{})
						if !ok {
							log.Printf("[WORKSPACE TOOLS] Warning: Failed to convert parameters for tool %s", tool.Function.Name)
							continue
						}

						underlyingAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							executor,
						)
					}
				}

				// Register human tools
				for _, tool := range humanTools {
					if executor, exists := humanExecutors[tool.Function.Name]; exists {
						// Type assert parameters to map[string]interface{}
						params, ok := tool.Function.Parameters.(map[string]interface{})
						if !ok {
							log.Printf("[HUMAN TOOLS] Warning: Failed to convert parameters for tool %s", tool.Function.Name)
							continue
						}

						underlyingAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							executor,
						)
					}
				}
			} else {
				// Register workspace and human tools for Simple agents too
				workspaceTools := virtualtools.CreateWorkspaceTools()
				workspaceExecutors := virtualtools.CreateWorkspaceToolExecutors()
				humanTools := virtualtools.CreateHumanTools()
				humanExecutors := virtualtools.CreateHumanToolExecutors()

				// Register workspace tools
				for _, tool := range workspaceTools {
					if executor, exists := workspaceExecutors[tool.Function.Name]; exists {
						// Type assert parameters to map[string]interface{}
						params, ok := tool.Function.Parameters.(map[string]interface{})
						if !ok {
							log.Printf("[WORKSPACE TOOLS] Warning: Failed to convert parameters for tool %s", tool.Function.Name)
							continue
						}

						underlyingAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							executor,
						)
					}
				}

				// Register human tools
				for _, tool := range humanTools {
					if executor, exists := humanExecutors[tool.Function.Name]; exists {
						// Type assert parameters to map[string]interface{}
						params, ok := tool.Function.Parameters.(map[string]interface{})
						if !ok {
							log.Printf("[HUMAN TOOLS] Warning: Failed to convert parameters for tool %s", tool.Function.Name)
							continue
						}

						underlyingAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							executor,
						)
					}
				}
			}
		}

		// Add event observer immediately after agent creation to capture all events
		// ✅ FIX: Always attach EventObserver to agent, even in orchestrator mode
		// The OrchestratorAgentEventBridge handles orchestrator-specific events, but we still need EventObserver for regular agent events
		log.Printf("[DATABASE DEBUG] Starting event observer setup for session %s", sessionID)
		log.Printf("[DATABASE DEBUG] ObserverID: %s", observerID)
		log.Printf("[DATABASE DEBUG] ChatDB available: %v", api.chatDB != nil)

		if observerID != "" {
			log.Printf("[DATABASE DEBUG] Creating in-memory event observer for session %s", sessionID)
			// Create in-memory event observer for real-time updates
			eventObserver := events.NewEventObserverWithLogger(api.eventStore, observerID, sessionID, api.logger)

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
				log.Printf("[POLLING DEBUG] Added event observer %s for session %s (new agent) - connected to underlying MCP agent", observerID, sessionID)
				log.Printf("[POLLING DEBUG] Added database event observer for session %s - events will be stored in database", sessionID)
			} else {
				log.Printf("[DATABASE DEBUG] ERROR: Underlying MCP agent is nil for session %s", sessionID)
				log.Printf("[POLLING DEBUG] Warning: Cannot add event observer - underlying MCP agent is nil")
			}
		} else {
			log.Printf("[DATABASE DEBUG] WARNING: ObserverID is empty for session %s, skipping event observer setup", sessionID)
		}

		// --- BEGIN: Load conversation history and accumulate for streaming ---
		// Load conversation history for this session
		api.conversationMux.RLock()
		history, exists := api.conversationHistory[sessionID]
		api.conversationMux.RUnlock()

		if exists && len(history) > 0 {
			log.Printf("[CONVERSATION DEBUG] Loading %d messages from conversation history for session %s", len(history), sessionID)
			// Load the conversation history into the agent
			for _, msg := range history {
				llmAgent.AppendMessage(msg)
			}
		} else {
			log.Printf("[CONVERSATION DEBUG] No conversation history found for session %s, starting fresh", sessionID)
		}

		// Add the current user message
		llmAgent.AppendUserMessage(req.Query)

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
			log.Printf("[AGENT DEBUG] llmAgent.StreamWithEvents() error: %v", err)
			sendError(fmt.Sprintf("Failed to start streaming: %v", err), true)
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
						log.Printf("[DATABASE DEBUG] Failed to update chat session status to error (timeout): %v", err)
					} else {
						log.Printf("[DATABASE DEBUG] Successfully updated chat session %s to error status (timeout)", sessionID)
					}
				}

				// Update active session status to error
				api.updateSessionStatus(sessionID, "error")

				// Emit server-level timeout completion event
				if observerID != "" {
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
					agentEvent.SessionID = observerID

					serverTimeoutEvent := events.Event{
						ID:        fmt.Sprintf("server_timeout_%s_%d", queryID, time.Now().UnixNano()),
						Type:      string(unifiedevents.EventTypeUnifiedCompletion),
						Timestamp: time.Now(),
						Data:      agentEvent,
						SessionID: observerID,
					}
					api.eventStore.AddEvent(observerID, serverTimeoutEvent)
					log.Printf("[SERVER DEBUG] Emitted server timeout completion event for query %s", queryID)
				}
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
			// Preserve the existing title and agent_mode
			completedAt := time.Now()
			updateReq := &database.UpdateChatSessionRequest{
				Title:       chatSession.Title,     // Preserve existing title
				AgentMode:   chatSession.AgentMode, // Preserve existing agent_mode
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

	// Handle orchestrator sessions with state preservation
	// Store planner orchestrator state before stopping
	api.orchestratorMux.RLock()
	if plannerOrch, exists := api.plannerOrchestrators[sessionID]; exists {
		// Get current state before stopping - need to get objective from stored state
		storedState, hasStoredState := api.getOrchestratorState(sessionID)
		var objective string
		if hasStoredState && storedState != nil {
			objective = storedState.Objective
		} else {
			// Fallback to empty string if no stored state
			objective = ""
		}

		state, err := plannerOrch.GetState(objective)
		if err == nil {
			// Store state for later restoration
			api.storeOrchestratorState(sessionID, state)
			log.Printf("[SESSION DEBUG] Saved planner orchestrator state for session %s", sessionID)
		} else {
			log.Printf("[SESSION DEBUG] Failed to get planner orchestrator state for session %s: %v", sessionID, err)
		}
	}
	api.orchestratorMux.RUnlock()

	// Store workflow orchestrator state before stopping
	api.orchestratorMux.RLock()
	if workflowOrch, exists := api.workflowOrchestrators[sessionID]; exists {
		// Get current state before stopping
		state, err := workflowOrch.GetState()
		if err == nil {
			// Store state for later restoration
			api.storeWorkflowState(sessionID, state)
			log.Printf("[SESSION DEBUG] Saved workflow orchestrator state for session %s", sessionID)
		} else {
			log.Printf("[SESSION DEBUG] Failed to get workflow orchestrator state for session %s: %v", sessionID, err)
		}
	}
	api.orchestratorMux.RUnlock()

	// Cancel orchestrator context if it exists
	api.orchestratorContextMux.Lock()
	if cancelFunc, exists := api.orchestratorContexts[sessionID]; exists {
		cancelFunc() // Cancel the orchestrator execution
		delete(api.orchestratorContexts, sessionID)
		log.Printf("[SESSION DEBUG] Cancelled orchestrator execution for session %s", sessionID)
	}
	api.orchestratorContextMux.Unlock()

	// Cancel workflow orchestrator context if it exists
	api.workflowOrchestratorContextMux.Lock()
	if cancelFunc, exists := api.workflowOrchestratorContexts[sessionID]; exists {
		cancelFunc() // Cancel the workflow orchestrator execution
		delete(api.workflowOrchestratorContexts, sessionID)
		log.Printf("[SESSION DEBUG] Cancelled workflow orchestrator execution for session %s", sessionID)
	}
	api.workflowOrchestratorContextMux.Unlock()

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

	// Clear orchestrator state
	api.clearOrchestratorState(sessionID)

	// Clear orchestrator instance (legacy removed)
	// Legacy orchestrator cleanup removed - now handled by plannerOrchestrators

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

// storeOrchestratorState stores the orchestrator state for later restoration
func (api *StreamingAPI) storeOrchestratorState(sessionID string, state *orchtypes.OrchestratorState) {
	api.orchestratorStateMux.Lock()
	defer api.orchestratorStateMux.Unlock()

	api.orchestratorStates[sessionID] = &StoredOrchestratorState{
		Type:         "planner",
		PlannerState: state,
		StoredAt:     time.Now(),
	}
	log.Printf("[SESSION DEBUG] Stored planner orchestrator state for session %s", sessionID)
}

// storeWorkflowState stores the workflow state for later restoration
func (api *StreamingAPI) storeWorkflowState(sessionID string, state *orchtypes.WorkflowState) {
	api.orchestratorStateMux.Lock()
	defer api.orchestratorStateMux.Unlock()

	api.orchestratorStates[sessionID] = &StoredOrchestratorState{
		Type:          "workflow",
		WorkflowState: state,
		StoredAt:      time.Now(),
	}
	log.Printf("[SESSION DEBUG] Stored workflow orchestrator state for session %s", sessionID)
}

// getOrchestratorState retrieves the stored orchestrator state
func (api *StreamingAPI) getOrchestratorState(sessionID string) (*orchtypes.OrchestratorState, bool) {
	api.orchestratorStateMux.RLock()
	defer api.orchestratorStateMux.RUnlock()

	storedState, exists := api.orchestratorStates[sessionID]
	if !exists || storedState.Type != "planner" || storedState.PlannerState == nil {
		return nil, false
	}
	return storedState.PlannerState, true
}

// getWorkflowState retrieves the stored workflow state
func (api *StreamingAPI) getWorkflowState(sessionID string) (*orchtypes.WorkflowState, bool) {
	api.orchestratorStateMux.RLock()
	defer api.orchestratorStateMux.RUnlock()

	storedState, exists := api.orchestratorStates[sessionID]
	if !exists || storedState.Type != "workflow" || storedState.WorkflowState == nil {
		return nil, false
	}
	return storedState.WorkflowState, true
}

// clearOrchestratorState clears the stored orchestrator state
func (api *StreamingAPI) clearOrchestratorState(sessionID string) {
	api.orchestratorStateMux.Lock()
	defer api.orchestratorStateMux.Unlock()

	if _, exists := api.orchestratorStates[sessionID]; exists {
		delete(api.orchestratorStates, sessionID)
		log.Printf("[SESSION DEBUG] Cleared orchestrator state for session %s", sessionID)
	}
}

// createServerLogger creates a logger instance for the server
func createServerLogger() utils.ExtendedLogger {
	serverLogger, err := logger.CreateLogger("", "info", "text", true)
	if err != nil {
		log.Fatalf("Failed to create server logger: %v", err)
	}
	return serverLogger
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

		sessions, total, err := db.ListChatSessions(r.Context(), limit, offset, presetQueryIDPtr)
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

// getSessionEventsHandler gets events for a specific session
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

		events, err := db.GetEventsBySession(r.Context(), sessionID, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"events": events,
			"total":  len(events),
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
func (api *StreamingAPI) trackActiveSession(sessionID, observerID, agentMode, query string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	api.activeSessions[sessionID] = &ActiveSessionInfo{
		SessionID:    sessionID,
		ObserverID:   observerID,
		AgentMode:    agentMode,
		Status:       "running",
		LastActivity: time.Now(),
		CreatedAt:    time.Now(),
		Query:        query,
	}

	log.Printf("[ACTIVE_SESSION] Tracked active session: %s (observer: %s, mode: %s)", sessionID, observerID, agentMode)
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

		// Remove completed sessions from activeSessions map
		if status == "completed" {
			api.activeSessionsMux.Lock()
			delete(api.activeSessions, sessionID)
			api.activeSessionsMux.Unlock()
			log.Printf("[ACTIVE_SESSION] Removed completed session %s from activeSessions", sessionID)
		}
	}()
}

// removeActiveSession removes an active session
func (api *StreamingAPI) removeActiveSession(sessionID string) {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	if _, exists := api.activeSessions[sessionID]; exists {
		delete(api.activeSessions, sessionID)
		log.Printf("[ACTIVE_SESSION] Removed active session: %s", sessionID)
	}
}

// getActiveSession retrieves an active session by ID
func (api *StreamingAPI) getActiveSession(sessionID string) (*ActiveSessionInfo, bool) {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()

	session, exists := api.activeSessions[sessionID]
	return session, exists
}

// getAllActiveSessions returns all active sessions
func (api *StreamingAPI) getAllActiveSessions() []*ActiveSessionInfo {
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()

	sessions := make([]*ActiveSessionInfo, 0, len(api.activeSessions))
	for _, session := range api.activeSessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// cleanupInactiveSessions removes sessions that haven't been active recently
func (api *StreamingAPI) cleanupInactiveSessions(maxInactiveTime time.Duration) int {
	api.activeSessionsMux.Lock()
	defer api.activeSessionsMux.Unlock()

	cutoff := time.Now().Add(-maxInactiveTime)
	removedCount := 0

	for sessionID, session := range api.activeSessions {
		if session.LastActivity.Before(cutoff) {
			delete(api.activeSessions, sessionID)
			removedCount++
			log.Printf("[ACTIVE_SESSION] Cleaned up inactive session: %s", sessionID)
		}
	}

	return removedCount
}

// storeWorkflowOrchestrator stores a workflow orchestrator for a session
func (api *StreamingAPI) storeWorkflowOrchestrator(sessionID string, orchestrator *orchtypes.WorkflowOrchestrator) {
	api.orchestratorMux.Lock()
	defer api.orchestratorMux.Unlock()
	api.workflowOrchestrators[sessionID] = orchestrator
	log.Printf("[ORCHESTRATOR] Stored workflow orchestrator for session %s", sessionID)
}

// storePlannerOrchestrator stores a planner orchestrator for a session
func (api *StreamingAPI) storePlannerOrchestrator(sessionID string, orchestrator *orchtypes.PlannerOrchestrator) {
	api.orchestratorMux.Lock()
	defer api.orchestratorMux.Unlock()
	api.plannerOrchestrators[sessionID] = orchestrator
	log.Printf("[ORCHESTRATOR] Stored planner orchestrator for session %s", sessionID)
}

// --- LLM GUIDANCE API HANDLERS ---

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
