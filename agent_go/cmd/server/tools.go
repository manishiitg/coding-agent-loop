package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpclient"
)

// --- TOOL MANAGEMENT TYPES ---

// ToolStatus represents the status of a tool
type ToolStatus struct {
	Name          string                 `json:"name"`
	Server        string                 `json:"server"`
	Status        string                 `json:"status"` // "ok", "loading", or "error"
	Error         string                 `json:"error,omitempty"`
	Description   string                 `json:"description,omitempty"`
	ToolsEnabled  int                    `json:"toolsEnabled"`
	FunctionNames []string               `json:"function_names"`
	Tools         []mcpclient.ToolDetail `json:"tools,omitempty"` // Only populated for detailed requests
}

// SetEnabledToolsRequest represents a request to set enabled tools
type SetEnabledToolsRequest struct {
	Enabled []string `json:"enabled_tools"`
	QueryID string   `json:"query_id,omitempty"`
}

// AddServerRequest represents a request to add a server
type AddServerRequest struct {
	Name   string                    `json:"name"`
	Server mcpclient.MCPServerConfig `json:"server"`
}

// EditServerRequest represents a request to edit a server
type EditServerRequest struct {
	Name   string                    `json:"name"`
	Server mcpclient.MCPServerConfig `json:"server"`
}

// RemoveServerRequest represents a request to remove a server
type RemoveServerRequest struct {
	Name string `json:"name"`
}

// --- TOOL MANAGEMENT FUNCTIONS ---

// checkAllToolStatus checks the status of all tools
func (api *StreamingAPI) checkAllToolStatus() {
	// Use dynamic tool discovery instead of hardcoded tools
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := api.discoverAllTools(ctx)

	api.toolStatusMux.Lock()
	// Preserve existing results and update/add new ones
	for _, result := range results {
		api.toolStatus[result.Name] = result
	}
	api.toolStatusMux.Unlock()
}

// discoverAllTools connects to all configured MCP servers IN PARALLEL and
// returns a consolidated slice of ToolStatus describing each server and its
// available tools. This avoids the sequential connection penalty that
// previously caused long wait-times when many servers were configured.
func (api *StreamingAPI) discoverAllTools(ctx context.Context) []ToolStatus {
	// Load merged config (base + user additions)
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Errorf("Failed to load merged config: %v", err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath)
		cfg = api.mcpConfig
	}
	results := mcpclient.DiscoverAllToolsParallel(ctx, cfg, api.logger)
	toolStatuses := make([]ToolStatus, 0, len(results))
	for _, r := range results {
		status := "ok"
		errMsg := ""
		fnNames := []string{}
		desc := ""
		if r.Error != nil {
			status = "error"
			errMsg = r.Error.Error()
		} else {
			for _, t := range r.Tools {
				fnNames = append(fnNames, t.Name)
			}
		}
		if srv, err := cfg.GetServer(r.ServerName); err == nil {
			desc = srv.Description
		}
		toolStatuses = append(toolStatuses, ToolStatus{
			Name:          r.ServerName,
			Server:        r.ServerName,
			Status:        status,
			Error:         errMsg,
			Description:   desc,
			ToolsEnabled:  len(fnNames),
			FunctionNames: fnNames,
		})
	}

	// Note: Comprehensive cache events are emitted by the agent system
	// during actual agent operations, not during administrative API calls
	// This ensures proper event routing to the observer system

	return toolStatuses
}

// discoverServerToolsDetailed connects to a specific MCP server and returns detailed tool information using mcpcache
func (api *StreamingAPI) discoverServerToolsDetailed(ctx context.Context, serverName string) (*ToolStatus, error) {
	// Load merged config to get server details
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Errorf("Failed to load merged config: %v", err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath)
		cfg = api.mcpConfig
	}

	// Get server configuration
	srvCfg, err := cfg.GetServer(serverName)
	if err != nil {
		return nil, fmt.Errorf("server not found: %s", serverName)
	}

	// Create temporary merged config file for mcpcache
	tmpConfigPath, err := api.createTempMergedConfig()
	if err != nil {
		api.logger.Errorf("Failed to create temp merged config: %v", err)
		// Fallback to base config path
		tmpConfigPath = api.mcpConfigPath
	} else {
		// Clean up temp file when done
		defer os.Remove(tmpConfigPath)
	}

	// Use mcpcache.GetCachedOrFreshConnection to get cached or fresh connection
	// This is the proper way to get MCP connections with caching
	result, err := mcpcache.GetCachedOrFreshConnection(
		ctx,
		nil, // No LLM needed for tool discovery
		serverName,
		tmpConfigPath, // Use temp merged config path
		nil,           // No tracers for server operations
		api.logger,
		false, // Default CacheOnly = false for server operations
	)
	if err != nil {
		return &ToolStatus{
			Name:         serverName,
			Server:       serverName,
			Status:       "error",
			Error:        err.Error(),
			Description:  srvCfg.Description,
			ToolsEnabled: 0,
		}, nil
	}

	// Extract tools for this specific server
	serverTools := api.extractServerTools(result.Tools, result.ToolToServer, serverName)

	// Convert to detailed format
	toolDetails := make([]mcpclient.ToolDetail, 0, len(serverTools))
	functionNames := make([]string, 0, len(serverTools))

	for _, tool := range serverTools {
		// llmtypes.Tool has a Function field that contains the actual tool information
		if tool.Function != nil {
			functionNames = append(functionNames, tool.Function.Name)

			toolDetail := mcpclient.ToolDetail{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  make(map[string]interface{}),
			}

			// Parse Parameters to extract properties and required fields
			if tool.Function.Parameters != nil {
				schemaBytes, err := json.Marshal(tool.Function.Parameters)
				if err == nil {
					var schemaMap map[string]interface{}
					if err := json.Unmarshal(schemaBytes, &schemaMap); err == nil {
						// Extract properties
						if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
							toolDetail.Parameters = props
						}

						// Extract required fields
						if req, ok := schemaMap["required"].([]interface{}); ok {
							for _, reqField := range req {
								if reqStr, ok := reqField.(string); ok {
									toolDetail.Required = append(toolDetail.Required, reqStr)
								}
							}
						}
					}
				}
			}

			toolDetails = append(toolDetails, toolDetail)
		}
	}

	return &ToolStatus{
		Name:          serverName,
		Server:        serverName,
		Status:        "ok",
		Description:   srvCfg.Description,
		ToolsEnabled:  len(serverTools),
		FunctionNames: functionNames,
		Tools:         toolDetails,
	}, nil
}

// --- TOOL MANAGEMENT API HANDLERS ---

// handleGetTools handles GET requests to retrieve all tools
func (api *StreamingAPI) handleGetTools(w http.ResponseWriter, r *http.Request) {
	// Return cached results immediately if available
	api.toolStatusMux.RLock()
	cachedResults := make([]ToolStatus, 0, len(api.toolStatus))
	for _, status := range api.toolStatus {
		cachedResults = append(cachedResults, status)
	}
	api.toolStatusMux.RUnlock()

	// Sort results alphabetically by server name
	sort.Slice(cachedResults, func(i, j int) bool {
		return cachedResults[i].Name < cachedResults[j].Name
	})

	// Always show all configured servers, not just cached ones
	// This ensures users see all servers including those that are loading or failed

	// Load merged config (base + user additions)
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Errorf("Failed to load merged config: %v", err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath)
		cfg = api.mcpConfig
	}

	// Create map of cached results for easy lookup
	cachedMap := make(map[string]ToolStatus)
	for _, status := range cachedResults {
		cachedMap[status.Name] = status
	}

	// Create comprehensive results showing ALL configured servers
	allResults := make([]ToolStatus, 0, len(cfg.MCPServers))
	for serverName, serverConfig := range cfg.MCPServers {
		if cachedStatus, exists := cachedMap[serverName]; exists {
			// Use cached result if available
			allResults = append(allResults, cachedStatus)
		} else {
			// Create fallback result for servers not yet discovered
			allResults = append(allResults, ToolStatus{
				Name:          serverName,
				Server:        serverName,
				Status:        "loading", // Indicate that tools are being discovered
				Description:   serverConfig.Description,
				ToolsEnabled:  0,
				FunctionNames: []string{},
			})
		}
	}

	// Sort results alphabetically by server name
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Name < allResults[j].Name
	})

	// Check if background discovery is running
	api.discoveryMux.RLock()
	isRunning := api.discoveryRunning
	api.discoveryMux.RUnlock()

	if !isRunning {
		api.logger.Infof("🔄 Starting background discovery for missing servers...")
		api.startBackgroundDiscovery()
	}

	// Return comprehensive results showing all servers
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allResults)
}

// handleGetToolDetail handles GET requests to retrieve detailed tool information for a specific server
func (api *StreamingAPI) handleGetToolDetail(w http.ResponseWriter, r *http.Request) {
	serverName := r.URL.Query().Get("server_name")
	if serverName == "" {
		http.Error(w, "server_name parameter is required", http.StatusBadRequest)
		return
	}

	// Check if we have cached detailed results for this server
	api.toolStatusMux.RLock()
	cachedStatus, exists := api.toolStatus[serverName]
	api.toolStatusMux.RUnlock()

	// If we have cached results with detailed tools, return them immediately
	if exists && len(cachedStatus.Tools) > 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&cachedStatus)
		return
	}

	// If no cached detailed results, fetch them and cache
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	result, err := api.discoverServerToolsDetailed(ctx, serverName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Cache the detailed results in mcpcache
	cacheManager := mcpcache.GetCacheManager(api.logger)

	// Get server config to generate proper cache key
	api.mcpConfig.ReloadConfig(api.mcpConfigPath)
	cfg := api.mcpConfig
	serverConfig, configErr := cfg.GetServer(serverName)
	if configErr != nil {
		api.logger.Warnf("⚠️ Failed to get server config for %s: %v", serverName, configErr)
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	cacheEntry := api.convertToolStatusToCacheEntry(result, serverName)
	if err := cacheManager.Put(cacheEntry, serverConfig); err != nil {
		api.logger.Warnf("⚠️ Failed to write detailed cache for server %s: %v", serverName, err)
	} else {
		api.logger.Infof("💾 Cached detailed tools for server: %s (%d tools)", serverName, len(result.Tools))
	}

	// Also update in-memory cache for immediate API responses
	api.toolStatusMux.Lock()
	api.toolStatus[serverName] = *result
	api.toolStatusMux.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleSetEnabledTools handles POST requests to set enabled tools
func (api *StreamingAPI) handleSetEnabledTools(w http.ResponseWriter, r *http.Request) {
	var req SetEnabledToolsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.QueryID == "" {
		http.Error(w, "Missing query_id", http.StatusBadRequest)
		return
	}
	api.toolStatusMux.Lock()
	api.enabledTools[req.QueryID] = req.Enabled
	api.toolStatusMux.Unlock()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// handleAddServer handles POST requests to add a server
func (api *StreamingAPI) handleAddServer(w http.ResponseWriter, r *http.Request) {
	var req AddServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := api.mcpConfig.AddServer(req.Name, req.Server, api.mcpConfigPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// handleEditServer handles POST requests to edit a server
func (api *StreamingAPI) handleEditServer(w http.ResponseWriter, r *http.Request) {
	var req EditServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := api.mcpConfig.EditServer(req.Name, req.Server, api.mcpConfigPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// handleRemoveServer handles POST requests to remove a server
func (api *StreamingAPI) handleRemoveServer(w http.ResponseWriter, r *http.Request) {
	var req RemoveServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := api.mcpConfig.RemoveServer(req.Name, api.mcpConfigPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// --- BACKGROUND TOOL DISCOVERY ---

// initializeToolCache initializes the tool cache on server startup using existing mcpcache service
func (api *StreamingAPI) initializeToolCache() {
	api.logger.Infof("🚀 Initializing tool cache on server startup using existing mcpcache service...")

	// Get the existing cache manager
	cacheManager := mcpcache.GetCacheManager(api.logger)

	// Load merged config (base + user additions)
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Errorf("Failed to load merged config: %v", err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath)
		cfg = api.mcpConfig
	}

	cachedServers := 0
	for serverName := range cfg.MCPServers {
		// Get server configuration for cache key generation
		serverConfig, exists := cfg.MCPServers[serverName]
		if !exists {
			continue
		}

		// Try to get cached entry using configuration-aware key
		cacheKey := mcpcache.GenerateUnifiedCacheKey(serverName, serverConfig)
		if entry, exists := cacheManager.Get(cacheKey); exists {
			cachedServers++
			// Convert cached entry to ToolStatus
			toolStatus := api.convertCacheEntryToToolStatus(entry)
			api.toolStatusMux.Lock()
			api.toolStatus[serverName] = toolStatus
			api.toolStatusMux.Unlock()
		}
	}

	if cachedServers > 0 {
		api.logger.Infof("✅ Loaded %d servers from existing mcpcache", cachedServers)
	}

	// Check if we need to discover more servers
	totalServers := len(cfg.MCPServers)
	if cachedServers < totalServers {
		missingServers := totalServers - cachedServers
		api.logger.Infof("🔄 Found %d cached servers, but config has %d servers. Starting background discovery for %d missing servers...",
			cachedServers, totalServers, missingServers)
		api.startBackgroundDiscovery()
	} else {
		api.logger.Infof("✅ All %d servers found in cache, starting periodic refresh only", cachedServers)
	}

	// Always start periodic refresh to keep cache updated
	api.startPeriodicRefresh()
}

// convertCacheEntryToToolStatus converts a mcpcache.CacheEntry to ToolStatus
func (api *StreamingAPI) convertCacheEntryToToolStatus(entry *mcpcache.CacheEntry) ToolStatus {
	functionNames := make([]string, 0, len(entry.Tools))
	toolDetails := make([]mcpclient.ToolDetail, 0, len(entry.Tools))

	for _, tool := range entry.Tools {
		// llmtypes.Tool has a Function field that contains the actual tool information
		if tool.Function != nil {
			functionNames = append(functionNames, tool.Function.Name)

			toolDetail := mcpclient.ToolDetail{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  make(map[string]interface{}),
			}

			// Parse Parameters to extract properties and required fields
			if tool.Function.Parameters != nil {
				schemaBytes, err := json.Marshal(tool.Function.Parameters)
				if err == nil {
					var schemaMap map[string]interface{}
					if err := json.Unmarshal(schemaBytes, &schemaMap); err == nil {
						// Extract properties
						if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
							toolDetail.Parameters = props
						}

						// Extract required fields
						if req, ok := schemaMap["required"].([]interface{}); ok {
							for _, reqField := range req {
								if reqStr, ok := reqField.(string); ok {
									toolDetail.Required = append(toolDetail.Required, reqStr)
								}
							}
						}
					}
				}
			}

			toolDetails = append(toolDetails, toolDetail)
		}
	}

	status := "ok"
	if !entry.IsValid {
		status = "error"
	}

	return ToolStatus{
		Name:          entry.ServerName,
		Server:        entry.ServerName,
		Status:        status,
		Error:         entry.ErrorMessage,
		ToolsEnabled:  len(entry.Tools),
		FunctionNames: functionNames,
		Tools:         toolDetails,
	}
}

// convertToolStatusToCacheEntry converts a ToolStatus to mcpcache.CacheEntry
func (api *StreamingAPI) convertToolStatusToCacheEntry(toolStatus *ToolStatus, serverName string) *mcpcache.CacheEntry {
	// Convert ToolDetail to llmtypes.Tool format using the centralized conversion function
	llmTools, err := mcpclient.ToolDetailsAsLLM(toolStatus.Tools)
	if err != nil {
		api.logger.Errorf("Failed to convert tool details to LLM tools: %v", err)
		// Return empty cache entry on error
		return &mcpcache.CacheEntry{
			ServerName:   serverName,
			Tools:        []llmtypes.Tool{},
			Prompts:      []mcp.Prompt{},
			Resources:    []mcp.Resource{},
			SystemPrompt: "",
			CreatedAt:    time.Now(),
			LastAccessed: time.Now(),
			TTLMinutes:   30,
			Protocol:     "unknown",
			ServerInfo:   make(map[string]interface{}),
			IsValid:      false,
			ErrorMessage: fmt.Sprintf("Tool conversion error: %v", err),
		}
	}

	// Create cache entry
	status := "ok"
	if toolStatus.Status == "error" {
		status = "error"
	}

	return &mcpcache.CacheEntry{
		ServerName:   serverName,
		Tools:        llmTools,
		Prompts:      []mcp.Prompt{},   // Empty for now
		Resources:    []mcp.Resource{}, // Empty for now
		SystemPrompt: "",               // Empty for now
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
		TTLMinutes:   30,        // 30 minutes TTL
		Protocol:     "unknown", // Will be updated by actual discovery
		ServerInfo:   make(map[string]interface{}),
		IsValid:      status == "ok",
		ErrorMessage: toolStatus.Error,
	}
}

// extractServerTools extracts tools specific to a server from the aggregated tool list
func (api *StreamingAPI) extractServerTools(allTools []llmtypes.Tool, toolToServer map[string]string, serverName string) []llmtypes.Tool {
	var serverTools []llmtypes.Tool
	for _, tool := range allTools {
		if tool.Function != nil {
			if srv, exists := toolToServer[tool.Function.Name]; exists && srv == serverName {
				serverTools = append(serverTools, tool)
			}
		}
	}
	return serverTools
}

// startBackgroundDiscovery starts the background tool discovery process
func (api *StreamingAPI) startBackgroundDiscovery() {
	api.discoveryMux.Lock()
	defer api.discoveryMux.Unlock()

	if api.discoveryRunning {
		return // Already running
	}

	api.discoveryRunning = true
	go api.runBackgroundDiscovery()
}

// runBackgroundDiscovery runs the actual background discovery using mcpcache service
func (api *StreamingAPI) runBackgroundDiscovery() {
	defer func() {
		api.discoveryMux.Lock()
		api.discoveryRunning = false
		api.discoveryMux.Unlock()
	}()

	api.logger.Infof("🔄 Starting background tool discovery using mcpcache service...")

	// Use a longer timeout for background discovery (5 minutes)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(api.logger)

	// Load merged config (base + user additions)
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Errorf("Failed to load merged config: %v", err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath)
		cfg = api.mcpConfig
	}

	discoveredServers := 0
	for serverName := range cfg.MCPServers {
		// Get server configuration for cache key generation
		serverConfig, err := cfg.GetServer(serverName)
		if err != nil {
			api.logger.Warnf("⚠️ Server %s not found in config, skipping: %v", serverName, err)
			continue
		}
		// Check if we already have valid cached data using configuration-aware key
		cacheKey := mcpcache.GenerateUnifiedCacheKey(serverName, serverConfig)
		if entry, exists := cacheManager.Get(cacheKey); exists {
			// Use existing cached data
			toolStatus := api.convertCacheEntryToToolStatus(entry)
			api.toolStatusMux.Lock()
			api.toolStatus[serverName] = toolStatus
			api.toolStatusMux.Unlock()
			discoveredServers++
			continue
		}

		// No valid cache, discover fresh data
		api.logger.Infof("🔍 Discovering tools for server: %s", serverName)

		// Use the existing discoverServerToolsDetailed function
		result, err := api.discoverServerToolsDetailed(ctx, serverName)
		if err != nil {
			api.logger.Warnf("⚠️ Failed to discover tools for server %s: %v", serverName, err)
			continue
		}

		// Convert ToolStatus to CacheEntry and write to mcpcache
		cacheEntry := api.convertToolStatusToCacheEntry(result, serverName)
		if err := cacheManager.Put(cacheEntry, serverConfig); err != nil {
			api.logger.Warnf("⚠️ Failed to write cache for server %s: %v", serverName, err)
		} else {
			api.logger.Infof("💾 Cached tools for server: %s (%d tools)", serverName, len(result.Tools))
		}

		// Update in-memory cache for immediate API responses
		api.toolStatusMux.Lock()
		api.toolStatus[serverName] = *result
		api.toolStatusMux.Unlock()

		discoveredServers++
	}

	api.lastDiscovery = time.Now()
	api.logger.Infof("✅ Background tool discovery completed: %d servers processed", discoveredServers)

	// Start periodic refresh (every 10 minutes)
	api.startPeriodicRefresh()
}

// startPeriodicRefresh starts periodic background refresh
func (api *StreamingAPI) startPeriodicRefresh() {
	api.discoveryMux.Lock()
	defer api.discoveryMux.Unlock()

	if api.discoveryTicker != nil {
		return // Already started
	}

	api.discoveryTicker = time.NewTicker(10 * time.Minute)
	go func() {
		for range api.discoveryTicker.C {
			api.logger.Infof("🔄 Starting periodic tool discovery refresh...")
			api.runBackgroundDiscovery()
		}
	}()

	api.logger.Infof("⏰ Started periodic tool discovery refresh (every 10 minutes)")
}

// stopPeriodicRefresh stops the periodic refresh
func (api *StreamingAPI) stopPeriodicRefresh() {
	api.discoveryMux.Lock()
	defer api.discoveryMux.Unlock()

	if api.discoveryTicker != nil {
		api.discoveryTicker.Stop()
		api.discoveryTicker = nil
		api.logger.Infof("⏹️ Stopped periodic tool discovery refresh")
	}
}
