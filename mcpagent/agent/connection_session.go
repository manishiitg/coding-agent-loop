// connection_session.go
//
// This file contains session-aware MCP connection management.
// When SessionID is provided, connections are stored in SessionConnectionRegistry
// and reused across agents in the same session.
//
// Exported:
//   - NewAgentConnectionWithSession: Session-aware connection that uses registry for connection reuse.

package mcpagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
)

// NewAgentConnectionWithSession creates MCP connections using the session registry.
// Connections are reused if they already exist for the session.
//
// When a connection already exists for (sessionID, serverName), it is reused.
// When a connection doesn't exist, it is created and stored in the registry.
//
// This function should be used when agents share connections within a workflow/conversation.
// Connections are NOT closed when the agent closes - call CloseSession(sessionID) at workflow end.
//
// Parameters:
//   - ctx: Context for connection timeout
//   - llm: LLM model for tool schema generation
//   - serverName: Server name(s) to connect to (comma-separated or "all")
//   - configPath: Path to MCP config file
//   - sessionID: Session ID for connection sharing
//   - traceID: Trace ID for observability
//   - tracers: Tracers for event emission
//   - logger: Logger for logging
//   - disableCache: If true, skip cache lookup for tool metadata (connections still reused via registry)
//   - userID: User ID for per-user OAuth token isolation (empty = use default from config)
//
// Returns:
//   - clients: Map of server name to client interface
//   - toolToServer: Map of tool name to server name
//   - tools: List of LLM tools
//   - servers: List of server names
//   - prompts: Map of server name to prompts
//   - resources: Map of server name to resources
//   - systemPrompt: Combined system prompt from servers
//   - error: Error if connection failed
func NewAgentConnectionWithSession(
	ctx context.Context,
	llm llmtypes.Model,
	serverName, configPath string,
	sessionID string,
	traceID string,
	tracers []observability.Tracer,
	logger loggerv2.Logger,
	disableCache bool,
	runtimeOverrides mcpclient.RuntimeOverrides,
	userID string,
) (map[string]mcpclient.ClientInterface, map[string]string, []llmtypes.Tool, []string, map[string][]mcp.Prompt, map[string][]mcp.Resource, string, error) {

	connectionStartTime := time.Now()

	logger.Info("NewAgentConnectionWithSession starting",
		loggerv2.String("session_id", sessionID),
		loggerv2.String("server_name", serverName),
		loggerv2.String("config_path", configPath))

	// Emit connection start event
	if len(tracers) > 0 {
		eventData := events.NewMCPServerConnectionEvent(serverName, "started", 0, 0, "")
		eventData.ConfigPath = configPath
		eventData.Operation = "connection_start_with_session"
		eventData.Timestamp = connectionStartTime

		event := events.NewAgentEvent(eventData)
		event.Type = events.MCPServerConnectionStart
		event.Timestamp = connectionStartTime
		event.TraceID = traceID
		event.CorrelationID = fmt.Sprintf("conn-session-%s-%s", sessionID, traceID)

		for _, tracer := range tracers {
			if err := tracer.EmitEvent(event); err != nil {
				logger.Warn("Failed to emit connection start event to tracer", loggerv2.Error(err))
			}
		}
	}

	// Load merged MCP configuration
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("failed to load merged MCP config: %w", err)
	}

	// Determine which servers to connect to
	var servers []string
	if serverName == "all" || serverName == "" {
		servers = config.ListServers()
		logger.Info("Using all servers", loggerv2.Int("server_count", len(servers)))
	} else if serverName == mcpclient.NoServers {
		servers = []string{}
		logger.Info("NoServers specified, returning empty connections")
	} else {
		for _, s := range strings.Split(serverName, ",") {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				servers = append(servers, trimmed)
			}
		}
		logger.Info("Using specific servers", loggerv2.Any("servers", servers))
	}

	// Handle special case: no servers requested
	if len(servers) == 0 {
		logger.Info("No servers requested, returning empty result")
		return make(map[string]mcpclient.ClientInterface), make(map[string]string), nil, servers, make(map[string][]mcp.Prompt), make(map[string][]mcp.Resource), "", nil
	}

	registry := mcpclient.GetSessionRegistry()
	clients := make(map[string]mcpclient.ClientInterface)
	toolToServer := make(map[string]string)
	var allTools []llmtypes.Tool
	prompts := make(map[string][]mcp.Prompt)
	resources := make(map[string][]mcp.Resource)
	var connectedServers []string

	// Track seen tools to prevent duplicates
	seenTools := make(map[string]bool)

	for _, srvName := range servers {
		serverConfig, err := config.GetServer(srvName)
		if err != nil {
			logger.Warn(fmt.Sprintf("Server %s not found in config, skipping", srvName),
				loggerv2.Error(err))
			continue
		}

		// Apply runtime overrides if provided for this server
		if runtimeOverrides != nil {
			if override, hasOverride := runtimeOverrides[srvName]; hasOverride {
				serverConfig = serverConfig.ApplyOverride(override)
				logger.Info("Applied runtime overrides to server config",
					loggerv2.String("server", srvName),
					loggerv2.Any("args_replace", override.ArgsReplace),
					loggerv2.Any("args_append", override.ArgsAppend),
					loggerv2.Any("env_override", override.EnvOverride))
			}
		}

		// Override OAuth token file path for per-user isolation
		if userID != "" && serverConfig.OAuth != nil {
			userTokenFile := fmt.Sprintf("~/.config/mcpagent/tokens/%s/%s.json", userID, srvName)
			serverConfig.OAuth.TokenFile = userTokenFile
			logger.Info("Using per-user OAuth token path",
				loggerv2.String("server", srvName),
				loggerv2.String("user_id", userID),
				loggerv2.String("token_file", userTokenFile))
		}

		// Use global shared session for stateless MCP servers.
		// Only stateful servers (e.g., playwright with browser state) need per-session connections.
		connSessionID := sessionID
		if srvName != "playwright" && srvName != "agent-browser" {
			connSessionID = "global"
		}

		// Get or create connection via registry
		client, wasCreated, err := registry.GetOrCreateConnection(ctx, connSessionID, srvName, serverConfig, logger)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to get/create connection for %s", srvName), err,
				loggerv2.String("session_id", sessionID))
			continue
		}

		clients[srvName] = client
		connectedServers = append(connectedServers, srvName)

		// Discover tools using ListTools (correct interface method)
		mcpTools, err := client.ListTools(ctx)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to discover tools for %s: %v", srvName, err))
			
			// 🔧 FALLBACK: Try to use cached tools when ListTools fails (e.g., connection closed)
			if !wasCreated {
				// Only try cache fallback for reused connections (new connections shouldn't have cache yet)
				logger.Info(fmt.Sprintf("Attempting to use cached tools for %s as fallback", srvName))
				
				cacheManager := mcpcache.GetCacheManager(logger)
				cacheKey := mcpcache.GenerateUnifiedCacheKey(srvName, serverConfig)
				
				if cachedEntry, exists := cacheManager.Get(cacheKey); exists && len(cachedEntry.Tools) > 0 {
					logger.Info(fmt.Sprintf("✅ Using %d cached tools for %s (fallback from failed ListTools)", 
						len(cachedEntry.Tools), srvName))
					
					// Use cached tools directly (they're already in llmtypes.Tool format)
					for _, llmTool := range cachedEntry.Tools {
						if llmTool.Function == nil {
							continue
						}
						toolName := llmTool.Function.Name
						
						// Check ToolOwnership to skip duplicates (if available)
						if cachedEntry.ToolOwnership != nil {
							if ownership, exists := cachedEntry.ToolOwnership[toolName]; exists && ownership == "duplicate" {
								logger.Debug("Skipping duplicate tool from cache",
									loggerv2.String("tool", toolName),
									loggerv2.String("server", srvName))
								continue
							}
						}
						
						// Runtime duplicate check (defensive)
						if seenTools[toolName] {
							logger.Warn(fmt.Sprintf("Duplicate tool %s from server %s (cached), skipping", toolName, srvName))
							continue
						}
						
						seenTools[toolName] = true
						allTools = append(allTools, llmTool)
						toolToServer[toolName] = srvName
					}
					
					// Set mcpTools to empty slice to indicate we used cache (for logging)
					mcpTools = []mcp.Tool{}
				} else {
					logger.Warn(fmt.Sprintf("No cached tools available for %s, continuing with empty tool list", srvName))
				}
			}
		} else {
			// Convert MCP tools to LLM tools using batch conversion
			llmTools, convErr := mcpclient.ToolsAsLLM(mcpTools)
			if convErr != nil {
				logger.Warn(fmt.Sprintf("Failed to convert tools for %s: %v", srvName, convErr))
			} else {
				for _, llmTool := range llmTools {
					if llmTool.Function == nil {
						continue
					}
					toolName := llmTool.Function.Name
					// Skip duplicates
					if seenTools[toolName] {
						logger.Warn(fmt.Sprintf("Duplicate tool %s from server %s, skipping", toolName, srvName))
						continue
					}
					seenTools[toolName] = true
					allTools = append(allTools, llmTool)
					toolToServer[toolName] = srvName
				}
			}
		}

		// Discover prompts using ListPrompts (correct interface method)
		if serverPrompts, err := client.ListPrompts(ctx); err == nil && len(serverPrompts) > 0 {
			prompts[srvName] = serverPrompts
		}

		// Discover resources using ListResources (correct interface method)
		if serverResources, err := client.ListResources(ctx); err == nil && len(serverResources) > 0 {
			resources[srvName] = serverResources
		}

		// Note: System prompt is not retrieved here - it comes from agent configuration
		// The ClientInterface doesn't expose GetSystemPrompt

		if wasCreated {
			logger.Info(fmt.Sprintf("New connection to %s (session=%s): %d tools discovered",
				srvName, sessionID, len(mcpTools)))
		} else {
			logger.Info(fmt.Sprintf("Reused connection to %s (session=%s): %d tools available",
				srvName, sessionID, len(mcpTools)))
		}
	}

	// System prompt is empty - it comes from agent configuration, not MCP servers
	systemPrompt := ""

	connectionDuration := time.Since(connectionStartTime)

	// Emit connection complete event
	if len(tracers) > 0 {
		eventData := events.NewMCPServerConnectionEvent(serverName, "completed_with_session", len(allTools), connectionDuration, "")
		eventData.ConfigPath = configPath
		eventData.Operation = "connection_complete_with_session"
		eventData.ServerInfo = map[string]interface{}{
			"session_id":      sessionID,
			"servers_count":   len(clients),
			"tools_count":     len(allTools),
			"prompts_count":   len(prompts),
			"resources_count": len(resources),
		}
		eventData.Timestamp = time.Now()

		event := events.NewAgentEvent(eventData)
		event.Type = events.MCPServerConnectionEnd
		event.Timestamp = eventData.Timestamp
		event.TraceID = traceID
		event.CorrelationID = fmt.Sprintf("conn-session-complete-%s-%s", sessionID, traceID)

		for _, tracer := range tracers {
			if err := tracer.EmitEvent(event); err != nil {
				logger.Warn("Failed to emit connection complete event to tracer", loggerv2.Error(err))
			}
		}
	}

	logger.Info("NewAgentConnectionWithSession completed",
		loggerv2.String("session_id", sessionID),
		loggerv2.Int("clients_count", len(clients)),
		loggerv2.Int("tools_count", len(allTools)),
		loggerv2.String("duration", connectionDuration.String()))

	return clients, toolToServer, allTools, connectedServers, prompts, resources, systemPrompt, nil
}
