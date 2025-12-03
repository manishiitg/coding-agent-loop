package mcpcache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcpagent/logger"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
)

// CachedConnectionResult represents the result of a cached or fresh MCP connection
type CachedConnectionResult struct {
	// Original connection data
	Clients      map[string]mcpclient.ClientInterface
	ToolToServer map[string]string
	Tools        []llmtypes.Tool
	Prompts      map[string][]mcp.Prompt
	Resources    map[string][]mcp.Resource
	SystemPrompt string

	// Cache metadata
	CacheUsed     bool
	CacheKey      string
	FreshFallback bool
	Error         error
}

// GenericCacheEvent represents a generic cache event to avoid circular imports
type GenericCacheEvent struct {
	Type           string        `json:"type"`
	ServerName     string        `json:"server_name,omitempty"`
	CacheKey       string        `json:"cache_key,omitempty"`
	ConfigPath     string        `json:"config_path,omitempty"`
	ToolsCount     int           `json:"tools_count,omitempty"`
	Age            time.Duration `json:"age,omitempty"`
	TTL            time.Duration `json:"ttl,omitempty"`
	DataSize       int64         `json:"data_size,omitempty"`
	Reason         string        `json:"reason,omitempty"`
	Operation      string        `json:"operation,omitempty"`
	Error          string        `json:"error,omitempty"`
	ErrorType      string        `json:"error_type,omitempty"`
	CleanupType    string        `json:"cleanup_type,omitempty"`
	EntriesRemoved int           `json:"entries_removed,omitempty"`
	EntriesTotal   int           `json:"entries_total,omitempty"`
	SpaceFreed     int64         `json:"space_freed,omitempty"`
	Timestamp      time.Time     `json:"timestamp"`
}

// GetType implements the observability.AgentEvent interface
func (e *GenericCacheEvent) GetType() string {
	return e.Type
}

// GetCorrelationID implements the observability.AgentEvent interface
func (e *GenericCacheEvent) GetCorrelationID() string {
	return ""
}

// GetTimestamp implements the observability.AgentEvent interface
func (e *GenericCacheEvent) GetTimestamp() time.Time {
	return e.Timestamp
}

// GetData implements the observability.AgentEvent interface
func (e *GenericCacheEvent) GetData() interface{} {
	return e
}

// GetTraceID implements the observability.AgentEvent interface
func (e *GenericCacheEvent) GetTraceID() string {
	return ""
}

// GetParentID implements the observability.AgentEvent interface
func (e *GenericCacheEvent) GetParentID() string {
	return ""
}

// Individual cache event types removed - only comprehensive cache events are used

// ComprehensiveCacheEvent represents a consolidated cache event with all details
type ComprehensiveCacheEvent struct {
	Type       string    `json:"type"`
	ServerName string    `json:"server_name"`
	ConfigPath string    `json:"config_path"`
	Timestamp  time.Time `json:"timestamp"`

	// Cache operation details
	Operation     string `json:"operation"`      // "start", "complete", "error"
	CacheUsed     bool   `json:"cache_used"`     // Whether cache was used
	FreshFallback bool   `json:"fresh_fallback"` // Whether fresh connections were used

	// Server details
	ServersCount   int `json:"servers_count"`
	TotalTools     int `json:"total_tools"`
	TotalPrompts   int `json:"total_prompts"`
	TotalResources int `json:"total_resources"`

	// Individual server cache status
	ServerStatus map[string]ServerCacheStatus `json:"server_status"`

	// Cache statistics
	CacheHits   int `json:"cache_hits"`
	CacheMisses int `json:"cache_misses"`
	CacheWrites int `json:"cache_writes"`
	CacheErrors int `json:"cache_errors"`

	// Performance metrics
	ConnectionTime string `json:"connection_time"`
	CacheTime      string `json:"cache_time"`

	// Error information
	Errors []string `json:"errors,omitempty"`
}

// ServerCacheStatus represents the cache status for a specific server
type ServerCacheStatus struct {
	ServerName     string `json:"server_name"`
	Status         string `json:"status"` // "hit", "miss", "write", "error"
	CacheKey       string `json:"cache_key,omitempty"`
	ToolsCount     int    `json:"tools_count"`
	PromptsCount   int    `json:"prompts_count"`
	ResourcesCount int    `json:"resources_count"`
	Age            string `json:"age,omitempty"`    // For cache hits
	Reason         string `json:"reason,omitempty"` // For cache misses
	Error          string `json:"error,omitempty"`  // For cache errors
}

// DuplicateToolFields represents typed fields for duplicate tool warning logs
type DuplicateToolFields struct {
	ToolName        string
	ExistingServer  string
	DuplicateServer string
}

// ToLogrusFields converts DuplicateToolFields to logrus.Fields for structured logging
func (f DuplicateToolFields) ToLogrusFields() logrus.Fields {
	return logrus.Fields{
		"tool_name":        f.ToolName,
		"existing_server":  f.ExistingServer,
		"duplicate_server": f.DuplicateServer,
	}
}

// Individual cache event interface implementations removed

// GetType implements the observability.AgentEvent interface
func (e *ComprehensiveCacheEvent) GetType() string {
	return e.Type
}

// GetCorrelationID implements the observability.AgentEvent interface
func (e *ComprehensiveCacheEvent) GetCorrelationID() string {
	return ""
}

// GetTimestamp implements the observability.AgentEvent interface
func (e *ComprehensiveCacheEvent) GetTimestamp() time.Time {
	return e.Timestamp
}

// GetData implements the observability.AgentEvent interface
func (e *ComprehensiveCacheEvent) GetData() interface{} {
	return e
}

// GetTraceID implements the observability.AgentEvent interface
func (e *ComprehensiveCacheEvent) GetTraceID() string {
	return ""
}

// GetParentID implements the observability.AgentEvent interface
func (e *ComprehensiveCacheEvent) GetParentID() string {
	return ""
}

// GetCachedOrFreshConnection attempts to get MCP connection data from cache first,
// falling back to fresh connection if cache is unavailable or expired
func GetCachedOrFreshConnection(
	ctx context.Context,
	llm llmtypes.Model,
	serverName, configPath string,
	tracers []observability.Tracer,
	logger logger.ExtendedLogger,
) (*CachedConnectionResult, error) {

	// Track cache operation start time
	cacheStartTime := time.Now()

	// Initialize server status tracking
	serverStatus := make(map[string]ServerCacheStatus)

	result := &CachedConnectionResult{
		Clients:      make(map[string]mcpclient.ClientInterface),
		ToolToServer: make(map[string]string),
		Prompts:      make(map[string][]mcp.Prompt),
		Resources:    make(map[string][]mcp.Resource),
	}

	// Get cache manager
	cacheManager := GetCacheManager(logger)

	// Load merged MCP configuration to get server details (base + user)
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load merged MCP config: %w", err)
	}

	// Determine which servers to connect to
	var servers []string
	if serverName == "all" || serverName == "" {
		servers = config.ListServers()
	} else if serverName == mcpclient.NoServers {
		// Special case: no servers should be connected
		servers = []string{}
		logger.Info("🔍 No servers requested - pure LLM reasoning mode", map[string]interface{}{
			"server_count": 0,
			"servers":      []string{},
		})
	} else {
		// Handle comma-separated server names
		requestedServers := strings.Split(serverName, ",")
		for _, reqServer := range requestedServers {
			reqServer = strings.TrimSpace(reqServer)
			// Check if this server exists in config
			for _, configServer := range config.ListServers() {
				if configServer == reqServer {
					servers = append(servers, reqServer)
					break
				}
			}
		}
	}

	logger.Info("🔍 Processing servers", map[string]interface{}{
		"server_count": len(servers),
		"servers":      servers,
	})

	// Handle special case: no servers requested (pure LLM reasoning)
	if len(servers) == 0 {
		logger.Info("✅ No servers requested - returning empty connection result", map[string]interface{}{
			"server_count": 0,
			"tools_count":  0,
			"cache_used":   true,
		})

		// Return empty result for pure LLM reasoning
		result.CacheUsed = true
		result.CacheKey = mcpclient.NoServers
		result.FreshFallback = false

		// Set empty collections (but preserve workspace tools)
		result.Clients = make(map[string]mcpclient.ClientInterface)
		result.ToolToServer = make(map[string]string)
		// Note: result.Tools is intentionally left empty - workspace tools are added separately
		result.Prompts = make(map[string][]mcp.Prompt)
		result.Resources = make(map[string][]mcp.Resource)
		// Note: result.SystemPrompt is intentionally left empty - agent will get proper system prompt from agent creation

		return result, nil
	}

	// Try to get data from cache for each server
	allFromCache := true
	var cachedData map[string]*CacheEntry
	var cachedServers []string
	var missedServers []string

	for _, srvName := range servers {
		_, exists := config.MCPServers[srvName]
		if !exists {
			return nil, fmt.Errorf("server %s not found in config", srvName)
		}

		// Get server configuration for cache key generation
		serverConfig, err := config.GetServer(srvName)
		if err != nil {
			logger.Warnf("Failed to get server config for %s: %v", srvName, err)
			continue
		}

		// Use configuration-aware cache key for consistency across all cache systems
		cacheKey := GenerateUnifiedCacheKey(srvName, serverConfig)

		// Try to get from cache using configuration-aware key
		if entry, found := cacheManager.Get(cacheKey); found {
			// Calculate cache age
			age := time.Since(entry.CreatedAt)

			logger.Info("✅ Cache hit", map[string]interface{}{
				"server":    srvName,
				"cache_key": cacheKey,
			})

			// Track cache hit status (no individual event emission)
			serverStatus[srvName] = ServerCacheStatus{
				ServerName:     srvName,
				Status:         "hit",
				CacheKey:       cacheKey,
				ToolsCount:     len(entry.Tools),
				PromptsCount:   len(entry.Prompts),
				ResourcesCount: len(entry.Resources),
				Age:            age.String(),
			}

			// Store cached data for later processing
			if cachedData == nil {
				cachedData = make(map[string]*CacheEntry)
			}
			cachedData[srvName] = entry
			cachedServers = append(cachedServers, srvName)
			result.CacheKey = cacheKey
			result.CacheUsed = true
		} else {
			logger.Info("❌ Cache miss", map[string]interface{}{
				"server":    srvName,
				"cache_key": cacheKey,
			})

			// Try to reload cache from disk before giving up
			logger.Info("🔄 Attempting to reload cache from disk", map[string]interface{}{
				"server":    srvName,
				"cache_key": cacheKey,
			})

			// Try to reload the cache entry from disk
			if reloadedEntry := cacheManager.ReloadFromDisk(cacheKey); reloadedEntry != nil {
				logger.Info("✅ Cache reloaded from disk", map[string]interface{}{
					"server":    srvName,
					"cache_key": cacheKey,
					"tools":     len(reloadedEntry.Tools),
				})

				// NOTE: Tools are already normalized before caching, no need to normalize again
				// This prevents race conditions from unlocked mutations

				// Use the reloaded entry
				age := time.Since(reloadedEntry.CreatedAt)
				serverStatus[srvName] = ServerCacheStatus{
					ServerName:     srvName,
					Status:         "hit",
					CacheKey:       cacheKey,
					ToolsCount:     len(reloadedEntry.Tools),
					PromptsCount:   len(reloadedEntry.Prompts),
					ResourcesCount: len(reloadedEntry.Resources),
					Age:            age.String(),
				}

				// Store cached data for later processing
				if cachedData == nil {
					cachedData = make(map[string]*CacheEntry)
				}
				cachedData[srvName] = reloadedEntry
				cachedServers = append(cachedServers, srvName)
				result.CacheKey = cacheKey
				result.CacheUsed = true
				continue // Skip to next server
			} else {
				logger.Warn("⚠️ Cache reload from disk failed", map[string]interface{}{
					"server":    srvName,
					"cache_key": cacheKey,
				})
			}

			// Track cache miss status (no individual event emission)
			serverStatus[srvName] = ServerCacheStatus{
				ServerName:     srvName,
				Status:         "miss",
				CacheKey:       cacheKey,
				ToolsCount:     0,
				PromptsCount:   0,
				ResourcesCount: 0,
				Reason:         "not_found",
			}

			missedServers = append(missedServers, srvName)
			allFromCache = false
			// DON'T BREAK - continue checking all servers for hybrid approach
		}
	}

	// HYBRID APPROACH: Handle different cache scenarios
	if allFromCache && len(cachedData) > 0 {
		// SCENARIO 1: All servers cached - use cached data
		logger.Info("🎯 All servers cached - using cached data (will still connect)", map[string]interface{}{
			"cached_servers": len(cachedData),
		})

		// Emit comprehensive cache event for cached data usage
		cacheTime := time.Since(cacheStartTime)
		EmitComprehensiveCacheEvent(
			tracers,
			"complete",
			configPath,
			servers,
			result,
			serverStatus,
			time.Duration(0), // Connection time not available here
			cacheTime,
			nil, // No errors at this point
		)

		return processCachedData(ctx, llm, cachedData, config, servers, configPath, tracers, logger)
	}

	// SCENARIO 2 & 3: Partial cache or all missed
	if len(cachedServers) > 0 && len(missedServers) > 0 {
		// HYBRID: Some cached, some missed - use cached data + connect only to missed servers
		logger.Info("🔀 Hybrid cache scenario - using cached + connecting to missed servers", map[string]interface{}{
			"cached_servers": cachedServers,
			"missed_servers": missedServers,
			"cached_count":   len(cachedServers),
			"missed_count":   len(missedServers),
		})

		// Start with cached data
		result.CacheUsed = true
		result.FreshFallback = true // Indicates hybrid mode

		// Process cached data first
		cachedResult, err := processCachedData(ctx, llm, cachedData, config, cachedServers, configPath, tracers, logger)
		if err != nil {
			logger.Warnf("Failed to process cached data in hybrid mode: %v", err)
			// Continue to fresh connection for missed servers anyway
		} else {
			// Copy cached result data
			result.Clients = cachedResult.Clients
			result.ToolToServer = cachedResult.ToolToServer
			result.Tools = cachedResult.Tools
			result.Prompts = cachedResult.Prompts
			result.Resources = cachedResult.Resources
			result.SystemPrompt = cachedResult.SystemPrompt
		}

		// Connect only to missed servers
		missedServersStr := strings.Join(missedServers, ",")
		freshResult, err := performFreshConnection(ctx, llm, missedServersStr, configPath, tracers, logger)
		if err != nil {
			logger.Errorf("Failed to connect to missed servers: %v", err)
			result.Error = err
			// Return partial result with cached data
			return result, fmt.Errorf("hybrid cache: cached %d servers, failed to connect to %d servers: %w", len(cachedServers), len(missedServers), err)
		}

		// Merge fresh results with cached results
		for serverName, client := range freshResult.Clients {
			result.Clients[serverName] = client
		}
		for toolName, serverName := range freshResult.ToolToServer {
			result.ToolToServer[toolName] = serverName
		}
		result.Tools = append(result.Tools, freshResult.Tools...)
		for serverName, prompts := range freshResult.Prompts {
			result.Prompts[serverName] = prompts
		}
		for serverName, resources := range freshResult.Resources {
			result.Resources[serverName] = resources
		}

		// Cache the fresh connection data for missed servers (async with timeout)
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cacheFreshConnectionData(cacheCtx, cacheManager, config, configPath, missedServers, freshResult, tracers, logger)
		}()

		logger.Info("✅ Hybrid cache complete", map[string]interface{}{
			"total_servers":  len(servers),
			"cached_servers": len(cachedServers),
			"fresh_servers":  len(missedServers),
			"total_tools":    len(result.Tools),
		})

		// Emit comprehensive cache event
		cacheTime := time.Since(cacheStartTime)
		EmitComprehensiveCacheEvent(
			tracers,
			"complete",
			configPath,
			servers,
			result,
			serverStatus,
			time.Duration(0),
			cacheTime,
			nil,
		)

		return result, nil
	}

	// SCENARIO 4: All servers missed cache - full fresh connection
	logger.Info("🔄 All servers missed cache - performing fresh connections", map[string]interface{}{
		"missed_servers": missedServers,
		"missed_count":   len(missedServers),
	})

	result.CacheUsed = false
	result.FreshFallback = true

	// Perform fresh connection (existing logic)
	freshResult, err := performFreshConnection(ctx, llm, serverName, configPath, tracers, logger)
	if err != nil {
		result.Error = err
		return result, err
	}

	// Copy fresh result data
	result.Clients = freshResult.Clients
	result.ToolToServer = freshResult.ToolToServer
	result.Tools = freshResult.Tools
	result.Prompts = freshResult.Prompts
	result.Resources = freshResult.Resources
	result.SystemPrompt = freshResult.SystemPrompt

	// Cache the fresh connection data (async with timeout)
	go func() {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cacheFreshConnectionData(cacheCtx, cacheManager, config, configPath, servers, freshResult, tracers, logger)
	}()

	// Emit comprehensive cache event with all details
	cacheTime := time.Since(cacheStartTime)
	EmitComprehensiveCacheEvent(
		tracers,
		"complete",
		configPath,
		servers,
		result,
		serverStatus,
		time.Duration(0), // Connection time not available here
		cacheTime,
		nil, // No errors at this point
	)

	return result, nil
}

// processCachedData processes cached entries and creates connections to servers
// Even when using cached tool definitions, we still connect to servers for execution
func processCachedData(
	ctx context.Context,
	llm llmtypes.Model,
	cachedData map[string]*CacheEntry,
	config *mcpclient.MCPConfig,
	servers []string,
	configPath string,
	tracers []observability.Tracer,
	logger logger.ExtendedLogger,
) (*CachedConnectionResult, error) {

	result := &CachedConnectionResult{
		Clients:      make(map[string]mcpclient.ClientInterface), // Will be populated with actual connections
		ToolToServer: make(map[string]string),
		Prompts:      make(map[string][]mcp.Prompt),
		Resources:    make(map[string][]mcp.Resource),
		CacheUsed:    true,
	}

	// Track seen tools to prevent duplicates (Gemini/Vertex rejects duplicate function declarations)
	seenTools := make(map[string]bool)

	// Aggregate data from all cached entries WITHOUT creating connections
	for _, srvName := range servers {
		entry, exists := cachedData[srvName]
		if !exists {
			continue
		}

		logger.Info("📋 Using cached data without connection", map[string]interface{}{
			"server":      srvName,
			"tools_count": len(entry.Tools),
			"protocol":    entry.Protocol,
		})

		// NOTE: Tools are already normalized before caching (see cacheFreshConnectionData)
		// No need to normalize again, which prevents race conditions from unlocked mutations
		logger.Debugf("Using pre-normalized tools for server %s: %d tools", srvName, len(entry.Tools))

		// Deduplicate tools using ToolOwnership metadata
		// Only add tools marked as "primary" for this server
		for _, t := range entry.Tools {
			if t.Function == nil {
				continue
			}

			toolName := t.Function.Name

			// Check ToolOwnership field to determine if this server should expose this tool
			if entry.ToolOwnership != nil {
				ownership, exists := entry.ToolOwnership[toolName]
				if exists && ownership == "duplicate" {
					// This tool is a duplicate - skip it with deterministic logging
					logger.Debugf("Skipping duplicate tool %s on server %s (marked as duplicate in cache)", toolName, srvName)
					continue
				}
				// If ownership is "primary" or not set, include the tool
			}

			// Runtime duplicate check (defensive - should not happen with ToolOwnership)
			if seenTools[toolName] {
				// Duplicate tool found despite ToolOwnership metadata - log warning
				existingServer := result.ToolToServer[toolName]
				fields := DuplicateToolFields{
					ToolName:        toolName,
					ExistingServer:  existingServer,
					DuplicateServer: srvName,
				}
				logger.WithFields(fields.ToLogrusFields()).Warn("⚠️ Unexpected duplicate tool in cache (ToolOwnership may need update)")
				continue
			}

			// This server owns this tool - add it
			seenTools[toolName] = true
			result.ToolToServer[toolName] = srvName
			result.Tools = append(result.Tools, t)
		}
		if entry.Prompts != nil {
			result.Prompts[srvName] = entry.Prompts
		}
		if entry.Resources != nil {
			result.Resources[srvName] = entry.Resources
		}

		logger.Info("✅ Cached data loaded", map[string]interface{}{
			"server":      srvName,
			"tools_count": len(entry.Tools),
		})
	}

	// Use cached system prompt if available
	if len(cachedData) > 0 {
		for _, entry := range cachedData {
			if entry.SystemPrompt != "" {
				result.SystemPrompt = entry.SystemPrompt
				break
			}
		}
	}

	// Now create actual connections to servers even though we're using cached tool definitions
	logger.Info("🔄 Creating actual connections to servers (using cached tool definitions)", map[string]interface{}{
		"servers": servers,
	})

	// Create connections using the original connection logic
	clients, _, _, _, prompts, resources, _, err := performOriginalConnectionLogic(ctx, llm, strings.Join(servers, ","), configPath, "cached-connection", tracers, logger)
	if err != nil {
		logger.Warn("⚠️ Failed to create connections, but continuing with cached data", map[string]interface{}{
			"error": err.Error(),
		})
		// Continue with cached data even if connections fail
	} else {
		// Use the actual connections
		result.Clients = clients
		// Merge discovered prompts and resources with cached ones
		for serverName, serverPrompts := range prompts {
			if existing, exists := result.Prompts[serverName]; exists {
				// Merge prompts (cached + fresh)
				result.Prompts[serverName] = append(existing, serverPrompts...)
			} else {
				result.Prompts[serverName] = serverPrompts
			}
		}
		for serverName, serverResources := range resources {
			if existing, exists := result.Resources[serverName]; exists {
				// Merge resources (cached + fresh)
				result.Resources[serverName] = append(existing, serverResources...)
			} else {
				result.Resources[serverName] = serverResources
			}
		}
	}

	logger.Info("✅ Cached data processing complete with connections", map[string]interface{}{
		"cached_servers": len(cachedData),
		"total_tools":    len(result.Tools),
		"connections":    len(result.Clients), // Actual connections created
	})

	return result, nil
}

// performFreshConnection performs the original fresh connection logic
func performFreshConnection(
	ctx context.Context,
	llm llmtypes.Model,
	serverName, configPath string,
	tracers []observability.Tracer,
	logger logger.ExtendedLogger,
) (*CachedConnectionResult, error) {

	// This would call the original NewAgentConnection function
	// For now, we'll simulate the call - in practice, this would be refactored
	clients, toolToServer, tools, _, prompts, resources, systemPrompt, err := performOriginalConnectionLogic(ctx, llm, serverName, configPath, "fresh-connection", tracers, logger)
	if err != nil {
		return nil, err
	}

	result := &CachedConnectionResult{
		Clients:      clients,
		ToolToServer: toolToServer,
		Tools:        tools,
		Prompts:      prompts,
		Resources:    resources,
		SystemPrompt: systemPrompt,
	}

	return result, nil
}

// performOriginalConnectionLogic contains the original connection logic
// This extracts and reimplements the original connection logic from NewAgentConnection
// Note: Simplified to avoid circular dependencies - no event emission
func performOriginalConnectionLogic(
	ctx context.Context,
	llm llmtypes.Model,
	serverName, configPath, traceID string,
	tracers []observability.Tracer,
	logger logger.ExtendedLogger,
) (map[string]mcpclient.ClientInterface, map[string]string, []llmtypes.Tool, []string, map[string][]mcp.Prompt, map[string][]mcp.Resource, string, error) {

	// Load merged MCP server configuration (base + user)
	logger.Info("🔍 Loading merged MCP config", map[string]interface{}{"config_path": configPath})
	cfg, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		logger.Error("❌ Failed to load merged MCP config", map[string]interface{}{"error": err.Error()})
		return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("load merged config: %w", err)
	}
	logger.Info("✅ Merged MCP config loaded", map[string]interface{}{"server_count": len(cfg.MCPServers)})

	// Determine which servers to connect to
	var servers []string
	if serverName == "all" || serverName == "" {
		servers = cfg.ListServers()
		logger.Info("🔍 Using all servers", map[string]interface{}{"server_count": len(servers)})
	} else {
		for _, s := range strings.Split(serverName, ",") {
			servers = append(servers, strings.TrimSpace(s))
		}
		logger.Info("🔍 Using specific servers", map[string]interface{}{"servers": servers})
	}

	clients := make(map[string]mcpclient.ClientInterface)
	toolToServer := make(map[string]string)
	var allLLMTools []llmtypes.Tool

	// Create a filtered config that only contains the specified servers
	filteredConfig := &mcpclient.MCPConfig{
		MCPServers: make(map[string]mcpclient.MCPServerConfig),
	}
	for _, serverName := range servers {
		if serverConfig, exists := cfg.MCPServers[serverName]; exists {
			filteredConfig.MCPServers[serverName] = serverConfig
		}
	}
	logger.Info("✅ Filtered config created", map[string]interface{}{"filtered_server_count": len(filteredConfig.MCPServers)})

	// Use new parallel tool discovery for only the specified servers
	discoveryStartTime := time.Now()
	logger.Info("🚀 Starting parallel tool discovery", map[string]interface{}{
		"server_count": len(filteredConfig.MCPServers),
		"servers":      servers,
		"start_time":   discoveryStartTime.Format(time.RFC3339),
	})

	// Log discovery start (events handled by connection.go)

	parallelResults := mcpclient.DiscoverAllToolsParallel(ctx, filteredConfig, logger)

	discoveryDuration := time.Since(discoveryStartTime)
	logger.Info("✅ Parallel tool discovery completed", map[string]interface{}{
		"result_count":   len(parallelResults),
		"discovery_time": discoveryDuration.String(),
		"discovery_ms":   discoveryDuration.Milliseconds(),
	})

	// Track seen tools to prevent duplicates (Gemini/Vertex rejects duplicate function declarations)
	seenTools := make(map[string]bool)

	for _, r := range parallelResults {
		srvName := r.ServerName

		srvCfg, err := cfg.GetServer(srvName)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("get server %s: %w", srvName, err)
		}

		if r.Error != nil {
			return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("connect to %s: %w", srvName, r.Error)
		}

		// Use the client from parallel tool discovery instead of creating a new one
		// This ensures we reuse the working SSE connection
		c := r.Client

		// For SSE connections, we already have a working connection from parallel discovery
		// For other protocols, we may need to reconnect
		if srvCfg.Protocol != mcpclient.ProtocolSSE {
			// Only reconnect for non-SSE protocols
			if err := c.ConnectWithRetry(ctx); err != nil {
				return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("connect to %s: %w", srvName, err)
			}
		}

		srvTools := r.Tools
		llmTools, err := mcpclient.ToolsAsLLM(srvTools)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("convert tools: %w", err)
		}

		// Tools are already normalized by ToolsAsLLM() during conversion
		// No extra normalization needed since langchaingo bug is fixed

		// Deduplicate tools: only add tools we haven't seen before
		for _, t := range llmTools {
			if t.Function == nil {
				continue
			}

			toolName := t.Function.Name
			if seenTools[toolName] {
				// Duplicate tool found - log warning and skip
				existingServer := toolToServer[toolName]
				fields := DuplicateToolFields{
					ToolName:        toolName,
					ExistingServer:  existingServer,
					DuplicateServer: srvName,
				}
				logger.WithFields(fields.ToLogrusFields()).Warn("⚠️ Duplicate tool detected, skipping")
				continue
			}

			// First occurrence of this tool - add it
			seenTools[toolName] = true
			toolToServer[toolName] = srvName
			allLLMTools = append(allLLMTools, t)
		}

		clients[srvName] = c
	}

	logger.Info("🔧 Aggregated tools", map[string]interface{}{
		"total_tools":     len(allLLMTools),
		"server_count":    len(clients),
		"connection_type": "direct",
	})

	// Discover prompts and resources from all connected servers
	allPrompts := make(map[string][]mcp.Prompt)
	allResources := make(map[string][]mcp.Resource)

	logger.Info("🔍 Discovering prompts and resources", map[string]interface{}{
		"server_count": len(clients),
	})
	for serverName, client := range clients {
		logger.Info("  📝 Checking prompts from server", map[string]interface{}{
			"server_name": serverName,
		})

		// For SSE connections, use the stored context from the client
		// For other protocols, use the parent context
		var discoveryCtx context.Context
		if client.GetContext() != nil {
			// Use stored context if available (SSE connections)
			discoveryCtx = client.GetContext()
			logger.Info("🔍 Using stored context for discovery", map[string]interface{}{"server_name": serverName})
		} else {
			// Fallback to parent context
			discoveryCtx = ctx
			logger.Info("🔍 Using parent context for discovery", map[string]interface{}{"server_name": serverName})
		}

		// Discover prompts
		prompts, err := client.ListPrompts(discoveryCtx)
		if err != nil {
			logger.Errorf("    ❌ Error listing prompts from %s: %v", serverName, err)
		} else if len(prompts) > 0 {
			// Fetch full content for each prompt
			var fullPrompts []mcp.Prompt
			for _, prompt := range prompts {
				// Try to get the full content
				promptResult, err := client.GetPrompt(discoveryCtx, prompt.Name)
				if err != nil {
					logger.Warnf("    ⚠️ Failed to get full content for prompt %s from %s: %v", prompt.Name, serverName, err)
					// Use the metadata prompt if full content fetch fails
					fullPrompts = append(fullPrompts, prompt)
				} else if promptResult != nil && len(promptResult.Messages) > 0 {
					// Extract content from messages
					var contentBuilder strings.Builder
					for _, msg := range promptResult.Messages {
						if textContent, ok := msg.Content.(*mcp.TextContent); ok {
							contentBuilder.WriteString(textContent.Text)
						} else if textContent, ok := msg.Content.(mcp.TextContent); ok {
							contentBuilder.WriteString(textContent.Text)
						}
					}
					fullContent := contentBuilder.String()
					if fullContent != "" {
						logger.Infof("    ✅ Fetched full content for prompt %s from %s (%d chars)", prompt.Name, serverName, len(fullContent))

						// Store full content in Description field (this will be used by virtual tools)
						// The system prompt builder will extract previews from this content
						fullPrompt := mcp.Prompt{
							Name:        prompt.Name,
							Description: fullContent, // Full content for virtual tools
						}
						fullPrompts = append(fullPrompts, fullPrompt)
					} else {
						// Fallback to metadata if content extraction fails
						fullPrompts = append(fullPrompts, prompt)
					}
				} else {
					// Fallback to metadata if prompt result is empty
					fullPrompts = append(fullPrompts, prompt)
				}
			}
			allPrompts[serverName] = fullPrompts
		}

		// Discover resources
		resources, err := client.ListResources(discoveryCtx)
		if err != nil {
			logger.Errorf("    ❌ Error listing resources from %s: %v", serverName, err)
		} else if len(resources) > 0 {
			allResources[serverName] = resources
			logger.Infof("    ✅ Found %d resources", len(resources))
		}
	}

	logger.Infof("📊 Summary: %d prompts, %d resources discovered",
		len(allPrompts), len(allResources))

	// Log detailed discovery completion (events handled by connection.go)

	// Build minimal system prompt (will be enhanced in agent creation)
	systemPrompt := fmt.Sprintf("Connected to %d MCP servers with %d tools available.",
		len(clients), len(allLLMTools))

	return clients, toolToServer, allLLMTools, servers, allPrompts, allResources, systemPrompt, nil
}

// cacheFreshConnectionData caches the results of a fresh connection
func cacheFreshConnectionData(
	ctx context.Context,
	cacheManager *CacheManager,
	config *mcpclient.MCPConfig,
	configPath string,
	servers []string,
	result *CachedConnectionResult,
	tracers []observability.Tracer,
	logger logger.ExtendedLogger,
) {
	for _, srvName := range servers {
		serverConfig, exists := config.MCPServers[srvName]
		if !exists {
			continue
		}

		// Extract server-specific tools
		serverTools := extractServerTools(result.Tools, result.ToolToServer, srvName)

		// IMPORTANT: Normalize tools BEFORE caching to ensure all array parameters have 'items' field
		// This prevents race conditions from normalizing after cache retrieval
		// Normalization is required for LLM providers (especially Gemini/Vertex)
		mcpclient.NormalizeLLMTools(serverTools)

		// Build ToolOwnership map to mark which tools this server owns
		// This prevents duplicate tool detection issues when loading from cache
		toolOwnership := make(map[string]string)
		for _, tool := range serverTools {
			toolName := tool.Function.Name
			owningServer, exists := result.ToolToServer[toolName]
			if !exists {
				// Tool not in ToolToServer map (shouldn't happen, but be defensive)
				logger.Warnf("Tool %s from server %s not found in ToolToServer map", toolName, srvName)
				toolOwnership[toolName] = "primary" // Assume primary by default
				continue
			}

			if owningServer == srvName {
				// This server is the primary owner of this tool
				toolOwnership[toolName] = "primary"
			} else {
				// Another server owns this tool (this server has a duplicate)
				toolOwnership[toolName] = "duplicate"
				logger.Debugf("Tool %s: Server %s marked as duplicate (primary: %s)", toolName, srvName, owningServer)
			}
		}

		// Create cache entry with pre-normalized tools and ownership info
		entry := &CacheEntry{
			ServerName:    srvName,
			Tools:         serverTools, // Already normalized
			Prompts:       result.Prompts[srvName],
			Resources:     result.Resources[srvName],
			SystemPrompt:  result.SystemPrompt,
			CreatedAt:     time.Now(),
			LastAccessed:  time.Now(),
			TTLMinutes:    cacheManager.GetTTL(), // Use configured TTL instead of hardcoded 30 minutes
			Protocol:      string(serverConfig.Protocol),
			IsValid:       true,
			ToolOwnership: toolOwnership, // Add ownership tracking
		}

		// Store in cache using configuration-aware cache key
		if err := cacheManager.Put(entry, serverConfig); err != nil {
			logger.Warnf("Failed to cache connection data for %s: %v", srvName, err)
		} else {
			logger.Debugf("Successfully cached connection data for %s (tools pre-normalized)", srvName)
		}
	}
}

// extractServerTools extracts tools specific to a server from the aggregated tool list
func extractServerTools(allTools []llmtypes.Tool, toolToServer map[string]string, serverName string) []llmtypes.Tool {
	var serverTools []llmtypes.Tool
	for _, tool := range allTools {
		if srv, exists := toolToServer[tool.Function.Name]; exists && srv == serverName {
			serverTools = append(serverTools, tool)
		}
	}
	return serverTools
}

// InvalidateServerCache invalidates cache entries for a specific server
func InvalidateServerCache(configPath, serverName string, logger logger.ExtendedLogger) error {
	cacheManager := GetCacheManager(logger)
	return cacheManager.InvalidateByServer(configPath, serverName)
}

// ClearAllCache clears all cache entries
func ClearAllCache(logger logger.ExtendedLogger) error {
	cacheManager := GetCacheManager(logger)
	return cacheManager.Clear()
}

// GetCacheStats returns cache statistics
func GetCacheStats(logger logger.ExtendedLogger) map[string]interface{} {
	cacheManager := GetCacheManager(logger)
	return cacheManager.GetStats()
}

// CleanupExpiredEntries removes expired cache entries
func CleanupExpiredEntries(logger logger.ExtendedLogger) error {
	cacheManager := GetCacheManager(logger)
	return cacheManager.Cleanup()
}

// ValidateCacheHealth validates the health of cached connections and emits events
func ValidateCacheHealth(tracers []observability.Tracer, logger logger.ExtendedLogger) {
	cacheManager := GetCacheManager(logger)
	stats := cacheManager.GetStats()

	logger.Info("🔍 Cache health check started", map[string]interface{}{
		"total_entries":   stats["total_entries"],
		"valid_entries":   stats["valid_entries"],
		"expired_entries": stats["expired_entries"],
	})

	// Cleanup expired entries
	if err := cacheManager.Cleanup(); err != nil {
		logger.Warnf("Cache cleanup failed: %w", err)
	} else {
		cleanupStats := cacheManager.GetStats()
		logger.Infof("Cache cleanup completed: %d expired entries removed", cleanupStats["expired_entries"])
	}
}

// ValidateServerCache validates cache for a specific server and emits events
func ValidateServerCache(serverName, configPath string, tracers []observability.Tracer, logger logger.ExtendedLogger) bool {
	cacheManager := GetCacheManager(logger)

	// Get merged server config to generate cache key
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		logger.Warnf("Failed to load merged config for cache validation: %w", err)
		return false
	}

	serverConfig, exists := config.MCPServers[serverName]
	if !exists {
		logger.Warnf("Server %s not found in config for cache validation", serverName)
		return false
	}

	cacheKey := GenerateUnifiedCacheKey(serverName, serverConfig)

	// Check if entry exists and is valid
	if entry, found := cacheManager.Get(cacheKey); found {
		age := time.Since(entry.CreatedAt)
		ttl := time.Duration(entry.TTLMinutes) * time.Minute

		if age < ttl {
			// Cache is valid
			logger.Debugf("Cache validation: %s is valid (age: %v, TTL: %v)", serverName, age, ttl)
			return true
		} else {
			// Cache expired - invalidate
			if err := cacheManager.InvalidateByServer(configPath, serverName); err != nil {
				logger.Warnf("Failed to invalidate cache for server %s: %v", serverName, err)
			}
			logger.Debugf("Cache validation: %s expired and invalidated", serverName)
			return false
		}
	} else {
		// Cache miss
		logger.Debugf("Cache validation: %s not found in cache", serverName)
		return false
	}
}

// GetCacheStatus returns detailed cache status for monitoring
func GetCacheStatus(configPath string, tracers []observability.Tracer, logger logger.ExtendedLogger) map[string]interface{} {
	cacheManager := GetCacheManager(logger)
	stats := cacheManager.GetStats()

	// Load merged config to get server list
	config, err := mcpclient.LoadMergedConfig(configPath, logger)
	if err != nil {
		logger.Warnf("Failed to load merged config for cache status: %w", err)
		return stats
	}

	// Add server-specific cache status
	serverStatus := make(map[string]interface{})
	for serverName := range config.MCPServers {
		serverConfig, exists := config.MCPServers[serverName]
		if !exists {
			continue
		}
		cacheKey := GenerateUnifiedCacheKey(serverName, serverConfig)

		if entry, found := cacheManager.Get(cacheKey); found {
			age := time.Since(entry.CreatedAt)
			ttl := time.Duration(entry.TTLMinutes) * time.Minute
			isValid := age < ttl

			serverStatus[serverName] = map[string]interface{}{
				"cached":          true,
				"cache_key":       cacheKey,
				"age":             age.String(),
				"ttl":             ttl.String(),
				"is_valid":        isValid,
				"tools_count":     len(entry.Tools),
				"prompts_count":   len(entry.Prompts),
				"resources_count": len(entry.Resources),
				"last_accessed":   entry.LastAccessed,
			}
		} else {
			serverStatus[serverName] = map[string]interface{}{
				"cached": false,
			}
		}
	}

	result := map[string]interface{}{
		"cache_stats":   stats,
		"server_status": serverStatus,
		"config_path":   configPath,
		"timestamp":     time.Now(),
	}

	return result
}

// EmitComprehensiveCacheEvent emits a single comprehensive cache event with all details
func EmitComprehensiveCacheEvent(
	tracers []observability.Tracer,
	operation string,
	configPath string,
	servers []string,
	result *CachedConnectionResult,
	serverStatus map[string]ServerCacheStatus,
	connectionTime time.Duration,
	cacheTime time.Duration,
	errors []string,
) {
	// Count cache statistics
	cacheHits := 0
	cacheMisses := 0
	cacheWrites := 0
	cacheErrors := 0

	for _, status := range serverStatus {
		switch status.Status {
		case "hit":
			cacheHits++
		case "miss":
			cacheMisses++
		case "write":
			cacheWrites++
		case "error":
			cacheErrors++
		}
	}

	// Calculate totals
	totalTools := 0
	totalPrompts := 0
	totalResources := 0

	if result != nil {
		totalTools = len(result.Tools)
		for _, prompts := range result.Prompts {
			totalPrompts += len(prompts)
		}
		for _, resources := range result.Resources {
			totalResources += len(resources)
		}
	}

	event := &ComprehensiveCacheEvent{
		Type:           "comprehensive_cache_event",
		ServerName:     "all-servers",
		ConfigPath:     configPath,
		Timestamp:      time.Now(),
		Operation:      operation,
		CacheUsed:      result != nil && result.CacheUsed,
		FreshFallback:  result != nil && result.FreshFallback,
		ServersCount:   len(servers),
		TotalTools:     totalTools,
		TotalPrompts:   totalPrompts,
		TotalResources: totalResources,
		ServerStatus:   serverStatus,
		CacheHits:      cacheHits,
		CacheMisses:    cacheMisses,
		CacheWrites:    cacheWrites,
		CacheErrors:    cacheErrors,
		ConnectionTime: connectionTime.String(),
		CacheTime:      cacheTime.String(),
		Errors:         errors,
	}

	for _, tracer := range tracers {
		_ = tracer.EmitEvent(event) // Silently ignore emission errors to avoid disrupting cache operations
	}
}

// Individual cache event functions removed - only comprehensive cache events are used
