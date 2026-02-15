// connection.go
//
// This file contains logic for establishing MCP client connections, discovering tools, and preparing all connection artifacts needed for agent construction.
//
// Exported:
//   - NewAgentConnection: Handles config loading, server connection, tool discovery, and returns clients, toolToServer map, tools, servers, and system prompt.

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

// NewAgentConnection handles MCP config loading, server connection, tool discovery, and returns all connection artifacts for agent construction.
// Now uses caching to avoid redundant connections and discoveries.
// Always connects to servers even when using cached data.
// If disableCache is true, skips cache lookup and always performs fresh connections.
// runtimeOverrides allows workflow-specific modifications to server configs (e.g., output directories)
// userID is accepted for API compatibility but per-user OAuth isolation requires the session-based path (use NewAgentConnectionWithSession)
func NewAgentConnection(ctx context.Context, llm llmtypes.Model, serverName, configPath, traceID string, tracers []observability.Tracer, logger loggerv2.Logger, disableCache bool, runtimeOverrides mcpclient.RuntimeOverrides, userID string) (map[string]mcpclient.ClientInterface, map[string]string, []llmtypes.Tool, []string, map[string][]mcp.Prompt, map[string][]mcp.Resource, string, error) {

	// Start timing the entire connection process
	connectionStartTime := time.Now()

	// Emit detailed connection start event
	if len(tracers) > 0 {
		eventData := events.NewMCPServerConnectionEvent(serverName, "started", 0, 0, "")
		eventData.ConfigPath = configPath
		eventData.Operation = "connection_start_with_cache"
		eventData.Timestamp = connectionStartTime

		event := events.NewAgentEvent(eventData)
		event.Type = events.MCPServerConnectionStart // Use correct unified event type
		event.Timestamp = connectionStartTime        // Preserve the actual connection start time
		event.TraceID = traceID
		event.CorrelationID = fmt.Sprintf("conn-start-%s", traceID)

		for _, tracer := range tracers {
			if err := tracer.EmitEvent(event); err != nil {
				logger.Warn("Failed to emit connection start event to tracer", loggerv2.Error(err))
			}
		}
	}

	cacheStatus := "with caching"
	if disableCache {
		cacheStatus = "cache disabled"
	}

	// Note: Per-user OAuth isolation (userID) is not supported in the legacy cache-based path
	// Use NewAgentConnectionWithSession for per-user OAuth token isolation
	if userID != "" {
		logger.Warn("userID set but legacy connection path does not support per-user OAuth isolation. Use session-based connections for per-user OAuth.",
			loggerv2.String("user_id", userID))
	}

	logger.Info("NewAgentConnection starting", loggerv2.String("server_name", serverName), loggerv2.String("config_path", configPath), loggerv2.Any("disable_cache", disableCache))
	logger.Info("NewAgentConnection started ("+cacheStatus+")",
		loggerv2.String("server_name", serverName),
		loggerv2.String("config_path", configPath),
		loggerv2.String("trace_id", traceID),
		loggerv2.String("start_time", connectionStartTime.Format(time.RFC3339)),
		loggerv2.String("disable_cache", fmt.Sprintf("%v", disableCache)))

	// Try to get cached or fresh connection data (always connects to servers)
	logger.Info("🔍 [DEBUG] NewAgentConnection: About to call GetCachedOrFreshConnection", loggerv2.String("server_name", serverName), loggerv2.String("config_path", configPath), loggerv2.Any("disable_cache", disableCache))
	getCacheStartTime := time.Now()
	result, err := mcpcache.GetCachedOrFreshConnection(ctx, llm, serverName, configPath, tracers, logger, disableCache, runtimeOverrides)
	getCacheDuration := time.Since(getCacheStartTime)
	if err != nil {
		logger.Error("❌ [DEBUG] NewAgentConnection: GetCachedOrFreshConnection failed", err, loggerv2.String("duration", getCacheDuration.String()), loggerv2.String("server_name", serverName))
	} else {
		logger.Info("✅ [DEBUG] NewAgentConnection: GetCachedOrFreshConnection completed successfully", loggerv2.String("duration", getCacheDuration.String()), loggerv2.Int("clients_count", len(result.Clients)), loggerv2.Int("tools_count", len(result.Tools)))
	}
	if err != nil {
		connectionDuration := time.Since(connectionStartTime)

		// Emit connection failure event
		if len(tracers) > 0 {
			failureTime := time.Now()
			eventData := events.NewMCPServerConnectionEvent(serverName, "failed", 0, connectionDuration, err.Error())
			eventData.ConfigPath = configPath
			eventData.Operation = "connection_failed"
			eventData.Timestamp = failureTime

			event := events.NewAgentEvent(eventData)
			event.Type = events.MCPServerConnectionError // Use correct unified event type
			event.Timestamp = failureTime                // Preserve the failure time
			event.TraceID = traceID
			event.CorrelationID = fmt.Sprintf("conn-failed-%s", traceID)

			for _, tracer := range tracers {
				if err := tracer.EmitEvent(event); err != nil {
					logger.Warn("Failed to emit connection failure event to tracer", loggerv2.Error(err))
				}
			}
		}

		return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("connection failed: %w", err)
	}

	// Determine servers list
	var servers []string
	if serverName == "all" || serverName == "" {
		servers = make([]string, 0, len(result.Clients))
		for srvName := range result.Clients {
			servers = append(servers, srvName)
		}
	} else {
		servers = strings.Split(serverName, ",")
		for i, s := range servers {
			servers[i] = strings.TrimSpace(s)
		}
	}

	// Calculate total connection duration
	connectionDuration := time.Since(connectionStartTime)

	// Count total prompts and resources across all servers
	totalPrompts := 0
	totalResources := 0
	for _, prompts := range result.Prompts {
		totalPrompts += len(prompts)
	}
	for _, resources := range result.Resources {
		totalResources += len(resources)
	}

	// Log comprehensive connection statistics
	if result.CacheUsed {
		logger.Info("Using cached connection data",
			loggerv2.Any("cache_used", result.CacheUsed),
			loggerv2.Any("fresh_fallback", result.FreshFallback),
			loggerv2.Int("servers_count", len(result.Clients)),
			loggerv2.Int("tools_count", len(result.Tools)),
			loggerv2.Int("prompts_count", totalPrompts),
			loggerv2.Int("resources_count", totalResources),
			loggerv2.String("cache_key", result.CacheKey),
			loggerv2.String("connection_time", connectionDuration.String()),
			loggerv2.Int("connection_ms", int(connectionDuration.Milliseconds())))
	} else {
		logger.Info("Using fresh connection data",
			loggerv2.Any("cache_used", false),
			loggerv2.Any("fresh_fallback", true),
			loggerv2.Int("servers_count", len(result.Clients)),
			loggerv2.Int("tools_count", len(result.Tools)),
			loggerv2.Int("prompts_count", totalPrompts),
			loggerv2.Int("resources_count", totalResources),
			loggerv2.String("connection_time", connectionDuration.String()),
			loggerv2.Int("connection_ms", int(connectionDuration.Milliseconds())))
	}

	// Emit detailed connection completion event
	if len(tracers) > 0 {
		status := "completed_with_cache"
		if !result.CacheUsed {
			status = "completed_fresh"
		}

		eventData := events.NewMCPServerConnectionEvent(serverName, status, len(result.Tools), connectionDuration, "")
		eventData.ConfigPath = configPath
		eventData.Operation = "connection_complete"
		eventData.ServerInfo = map[string]interface{}{
			"cache_used":          result.CacheUsed,
			"fresh_fallback":      result.FreshFallback,
			"servers_count":       len(result.Clients),
			"tools_count":         len(result.Tools),
			"prompts_count":       totalPrompts,
			"resources_count":     totalResources,
			"connection_time_ms":  connectionDuration.Milliseconds(),
			"cache_key":           result.CacheKey,
			"discovery_completed": true,
		}

		completionTime := time.Now()
		eventData.Timestamp = completionTime

		event := events.NewAgentEvent(eventData)
		event.Type = events.MCPServerConnectionEnd // Use correct unified event type
		event.Timestamp = completionTime           // Preserve the actual completion time
		event.TraceID = traceID
		event.CorrelationID = fmt.Sprintf("conn-complete-%s", traceID)

		for _, tracer := range tracers {
			if err := tracer.EmitEvent(event); err != nil {
				logger.Warn("Failed to emit connection complete event to tracer", loggerv2.Error(err))
			}
		}

		// Emit discovery event for detailed discovery statistics
		discoveryEventData := events.NewMCPServerDiscoveryEvent(len(result.Clients), len(result.Clients), 0, connectionDuration)
		discoveryEventData.ServerName = serverName
		discoveryEventData.Operation = "mcp_discovery_complete"
		discoveryEventData.ToolCount = len(result.Tools)
		discoveryEventData.Timestamp = completionTime

		discoveryEvent := events.NewAgentEvent(discoveryEventData)
		discoveryEvent.Type = events.MCPServerDiscovery // Use correct unified event type
		discoveryEvent.Timestamp = completionTime       // Use same completion time
		discoveryEvent.TraceID = traceID
		discoveryEvent.CorrelationID = fmt.Sprintf("discovery-complete-%s", traceID)

		for _, tracer := range tracers {
			if err := tracer.EmitEvent(discoveryEvent); err != nil {
				logger.Warn("Failed to emit discovery complete event to tracer", loggerv2.Error(err))
			}
		}
	}

	return result.Clients, result.ToolToServer, result.Tools, servers, result.Prompts, result.Resources, result.SystemPrompt, nil
}
