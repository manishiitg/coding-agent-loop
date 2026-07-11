package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/oauth"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// --- SERVER LOG TYPES ---

// ServerLogEntry represents a single log entry for an MCP server
type ServerLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // "info", "error", "warn", "debug"
	Message   string    `json:"message"`
}

// maxServerLogEntries is the maximum number of log entries kept per server
const maxServerLogEntries = 100

// serverLogs stores per-server log entries (managed on StreamingAPI)
// serverLogsMux protects concurrent access to serverLogs

// appendServerLog adds a log entry for a server, capping at maxServerLogEntries
func (api *StreamingAPI) appendServerLog(serverName, level, message string) {
	api.serverLogsMux.Lock()
	defer api.serverLogsMux.Unlock()

	entry := ServerLogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}

	logs := api.serverLogs[serverName]
	logs = append(logs, entry)
	if len(logs) > maxServerLogEntries {
		logs = logs[len(logs)-maxServerLogEntries:]
	}
	api.serverLogs[serverName] = logs
}

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
	// OAuth detection
	RequiresOAuth  bool            `json:"requires_oauth,omitempty"`  // Auto-detected from 401 response
	OAuthEndpoints *OAuthEndpoints `json:"oauth_endpoints,omitempty"` // Discovered endpoints if OAuth detected
}

// OAuthEndpoints represents discovered OAuth endpoints
type OAuthEndpoints struct {
	AuthURL  string `json:"auth_url"`
	TokenURL string `json:"token_url"`
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

// discoverServerToolsDetailed connects to a specific MCP server and returns detailed tool information using mcpcache
func (api *StreamingAPI) discoverServerToolsDetailed(ctx context.Context, serverName string) (*ToolStatus, error) {
	api.appendServerLog(serverName, "info", "Loading configuration...")

	// Load merged config to get server details
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.appendServerLog(serverName, "error", fmt.Sprintf("Failed to load merged config: %v", err))
		api.logger.Error(fmt.Sprintf("Failed to load merged config: %v", err), err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath, api.logger)
		cfg = api.mcpConfig
	}

	// Get server configuration
	srvCfg, err := cfg.GetServer(serverName)
	if err != nil {
		api.appendServerLog(serverName, "error", fmt.Sprintf("Server not found in configuration: %s", serverName))
		return nil, fmt.Errorf("server not found: %s", serverName)
	}

	// Determine protocol for logging
	protocol := "stdio"
	if srvCfg.URL != "" {
		protocol = "http"
		if srvCfg.Protocol == mcpclient.ProtocolSSE {
			protocol = "sse"
		}
	}
	api.appendServerLog(serverName, "info", fmt.Sprintf("Connecting to server (protocol: %s)...", protocol))

	// Create temporary merged config file for mcpcache
	tmpConfigPath, err := api.createTempMergedConfig()
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to create temp merged config: %v", err), err)
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
		false, // disableCache - use cache by default for server operations
		nil,   // No runtime overrides for tool discovery
	)
	if err != nil {
		api.appendServerLog(serverName, "error", fmt.Sprintf("Connection failed: %v", err))

		// Check if this is an OAuth error - try to auto-discover OAuth endpoints
		toolStatus := &ToolStatus{
			Name:         serverName,
			Server:       serverName,
			Status:       "error",
			Error:        err.Error(),
			Description:  srvCfg.Description,
			ToolsEnabled: 0,
		}

		// Try OAuth auto-discovery if server has URL (HTTP/SSE protocol)
		// Also support mcp-remote pattern: command=npx, args=["mcp-remote", "<url>"]
		discoveryURL := srvCfg.URL
		if discoveryURL == "" {
			discoveryURL = extractMCPRemoteURL(srvCfg.Command, srvCfg.Args)
		}
		if discoveryURL != "" {
			if endpoints := api.tryOAuthDiscovery(ctx, discoveryURL); endpoints != nil {
				toolStatus.RequiresOAuth = true
				toolStatus.OAuthEndpoints = endpoints
				toolStatus.Error = "OAuth authentication required"
				api.appendServerLog(serverName, "warn", "OAuth authentication required")
				api.logger.Info(fmt.Sprintf("✅ Auto-detected OAuth for %s: auth=%s, token=%s",
					serverName, endpoints.AuthURL, endpoints.TokenURL))
			}
		}

		return toolStatus, nil
	}

	api.appendServerLog(serverName, "info", "Connection established")
	api.appendServerLog(serverName, "info", "Discovering tools...")

	// Close MCP connections after extracting tool metadata.
	// Background discovery only needs tool names/schemas (metadata), not live connections.
	// Leaving connections open spawns subprocess per server that stays resident,
	// doubling memory usage and causing OOM.
	defer func() {
		for srvName, client := range result.Clients {
			if client != nil {
				if err := client.Close(); err != nil {
					api.logger.Debug(fmt.Sprintf("Failed to close MCP client for %s: %v", srvName, err))
				}
			}
		}
	}()

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

	api.appendServerLog(serverName, "info", fmt.Sprintf("Found %d tools", len(serverTools)))

	toolStatus := &ToolStatus{
		Name:          serverName,
		Server:        serverName,
		Status:        "ok",
		Description:   srvCfg.Description,
		ToolsEnabled:  len(serverTools),
		FunctionNames: functionNames,
		Tools:         toolDetails,
	}

	// For mcp-remote servers: probe the remote URL for OAuth even on successful connection.
	// Kite and similar servers allow unauthenticated tool listing but require auth for tool calls.
	// Only do this if OAuth is not already configured for the server.
	if srvCfg.OAuth == nil && srvCfg.URL == "" {
		if remoteURL := extractMCPRemoteURL(srvCfg.Command, srvCfg.Args); remoteURL != "" {
			if endpoints := api.tryOAuthDiscovery(ctx, remoteURL); endpoints != nil {
				toolStatus.RequiresOAuth = true
				toolStatus.OAuthEndpoints = endpoints
				api.appendServerLog(serverName, "warn", "OAuth authentication required (detected via mcp-remote URL)")
				api.logger.Info(fmt.Sprintf("✅ Auto-detected OAuth for mcp-remote server %s: auth=%s, token=%s",
					serverName, endpoints.AuthURL, endpoints.TokenURL))
			}
		}
	}

	return toolStatus, nil
}

// --- TOOL MANAGEMENT API HANDLERS ---

// handleGetTools handles GET requests to retrieve all tools
func (api *StreamingAPI) handleGetTools(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context for per-user OAuth status
	userID := GetUserIDFromContext(r.Context())

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
		api.logger.Error(fmt.Sprintf("Failed to load merged config: %v", err), err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath, api.logger)
		cfg = api.mcpConfig
	}

	// Create map of cached results for easy lookup
	cachedMap := make(map[string]ToolStatus)
	for _, status := range cachedResults {
		cachedMap[status.Name] = status
	}

	// Create comprehensive results showing ALL configured servers
	// Apply per-user OAuth status to each result
	allResults := make([]ToolStatus, 0, len(cfg.MCPServers))
	for serverName, serverConfig := range cfg.MCPServers {
		if cachedStatus, exists := cachedMap[serverName]; exists {
			// Use cached result but apply user-specific OAuth status
			userStatus := api.getToolStatusForUser(cachedStatus, userID)
			allResults = append(allResults, userStatus)
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
		api.logger.Info("🔄 Starting background discovery for missing servers...")
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

	// Get user ID from context for per-user OAuth status
	userID := GetUserIDFromContext(r.Context())

	// Check if we have cached detailed results for this server
	api.toolStatusMux.RLock()
	cachedStatus, exists := api.toolStatus[serverName]
	api.toolStatusMux.RUnlock()

	// If we have cached results with detailed tools, return them with user-specific OAuth status
	if exists && len(cachedStatus.Tools) > 0 {
		userStatus := api.getToolStatusForUser(cachedStatus, userID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&userStatus)
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
	api.mcpConfig.ReloadConfig(api.mcpConfigPath, api.logger)
	cfg := api.mcpConfig
	serverConfig, configErr := cfg.GetServer(serverName)
	if configErr != nil {
		api.logger.Warn(fmt.Sprintf("⚠️ Failed to get server config for %s: %v", serverName, configErr))
		http.Error(w, "Server configuration error", http.StatusInternalServerError)
		return
	}

	cacheEntry := api.convertToolStatusToCacheEntry(result, serverName)
	if err := cacheManager.Put(cacheEntry, serverConfig); err != nil {
		api.logger.Warn(fmt.Sprintf("⚠️ Failed to write detailed cache for server %s: %v", serverName, err))
	} else {
		api.logger.Info(fmt.Sprintf("💾 Cached detailed tools for server: %s (%d tools)", serverName, len(result.Tools)))
	}

	// Also update in-memory cache for immediate API responses
	api.toolStatusMux.Lock()
	api.toolStatus[serverName] = *result
	api.toolStatusMux.Unlock()

	// Return result with user-specific OAuth status
	userStatus := api.getToolStatusForUser(*result, userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&userStatus)
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
	// Check if MCP config is locked
	if isMCPConfigLocked() {
		http.Error(w, "MCP configuration is locked by administrator", http.StatusForbidden)
		return
	}

	var req AddServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := api.mcpConfig.AddServer(req.Name, req.Server, api.mcpConfigPath); err != nil {
		api.appendServerLog(req.Name, "error", fmt.Sprintf("Failed to add server: %v", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	api.appendServerLog(req.Name, "info", "Server added to configuration")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// handleEditServer handles POST requests to edit a server
func (api *StreamingAPI) handleEditServer(w http.ResponseWriter, r *http.Request) {
	// Check if MCP config is locked
	if isMCPConfigLocked() {
		http.Error(w, "MCP configuration is locked by administrator", http.StatusForbidden)
		return
	}

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
	// Check if MCP config is locked
	if isMCPConfigLocked() {
		http.Error(w, "MCP configuration is locked by administrator", http.StatusForbidden)
		return
	}

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

// discoveryFailedFile is the on-disk format for persisted discovery failures.
// It includes a config hash so that manual edits to the JSON config file
// automatically invalidate the persisted failures.
type discoveryFailedFile struct {
	ConfigHash string            `json:"config_hash"`
	Servers    map[string]string `json:"servers"`
}

// configFileHash returns a SHA-256 hash of the user MCP config file contents.
// If the file changes (manual edit, API update, etc.) the hash changes.
func (api *StreamingAPI) configFileHash() string {
	userConfigPath := strings.TrimSuffix(api.mcpConfigPath, ".json") + "_user.json"
	data, err := os.ReadFile(userConfigPath)
	if err != nil {
		// Fall back to base config
		data, err = os.ReadFile(api.mcpConfigPath)
		if err != nil {
			return ""
		}
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// persistDiscoveryFailedServers saves the failed servers map to disk so it
// survives server restarts. This prevents wasting ~19s per OAuth-failing server
// on every startup. A config hash is stored alongside so that manual config
// edits automatically invalidate the persisted failures.
func (api *StreamingAPI) persistDiscoveryFailedServers() {
	cacheManager := mcpcache.GetCacheManager(api.logger)
	filePath := filepath.Join(cacheManager.GetCacheDirectory(), "discovery_failed_servers.json")

	payload := discoveryFailedFile{
		ConfigHash: api.configFileHash(),
		Servers:    api.discoveryFailedServers,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		api.logger.Warn(fmt.Sprintf("Failed to marshal discoveryFailedServers: %v", err))
		return
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		api.logger.Warn(fmt.Sprintf("Failed to create cache directory: %v", err))
		return
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		api.logger.Warn(fmt.Sprintf("Failed to persist discoveryFailedServers: %v", err))
		return
	}

	api.logger.Info(fmt.Sprintf("Persisted %d failed servers to disk", len(api.discoveryFailedServers)))
}

// loadDiscoveryFailedServers loads previously failed servers from disk.
// If the MCP config file has changed since the failures were persisted,
// the file is discarded so all servers are retried.
func (api *StreamingAPI) loadDiscoveryFailedServers() {
	cacheManager := mcpcache.GetCacheManager(api.logger)
	filePath := filepath.Join(cacheManager.GetCacheDirectory(), "discovery_failed_servers.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			api.logger.Warn(fmt.Sprintf("Failed to read discoveryFailedServers from disk: %v", err))
		}
		return
	}

	var payload discoveryFailedFile
	if err := json.Unmarshal(data, &payload); err != nil {
		api.logger.Warn(fmt.Sprintf("Failed to unmarshal discoveryFailedServers: %v", err))
		return
	}

	// If config changed, discard stale failures so servers are retried
	currentHash := api.configFileHash()
	if payload.ConfigHash != currentHash {
		api.logger.Info("MCP config changed since last run — clearing persisted failed servers")
		_ = os.Remove(filePath)
		return
	}

	api.discoveryFailedServers = payload.Servers
	api.logger.Info(fmt.Sprintf("Loaded %d previously failed servers from disk", len(payload.Servers)))
	for name, reason := range payload.Servers {
		api.logger.Debug(fmt.Sprintf("  Previously failed: %s — %s", name, reason))
	}
}

// deleteDiscoveryFailedServersFile removes the persisted file (used on config reload).
func (api *StreamingAPI) deleteDiscoveryFailedServersFile() {
	cacheManager := mcpcache.GetCacheManager(api.logger)
	filePath := filepath.Join(cacheManager.GetCacheDirectory(), "discovery_failed_servers.json")
	_ = os.Remove(filePath)
}

func (api *StreamingAPI) clearDiscoveryFailure(serverName string) {
	if _, failed := api.discoveryFailedServers[serverName]; !failed {
		return
	}

	delete(api.discoveryFailedServers, serverName)
	if len(api.discoveryFailedServers) == 0 {
		api.deleteDiscoveryFailedServersFile()
	} else {
		api.persistDiscoveryFailedServers()
	}
	api.logger.Info(fmt.Sprintf("🔄 Cleared failed discovery state for %s", serverName))
}

// hasOAuthTokenFile checks whether a server's OAuth token file exists on disk.
// Returns true if the server doesn't use OAuth or if the token file is present.
func hasOAuthTokenFile(cfg mcpclient.MCPServerConfig) bool {
	if cfg.OAuth == nil {
		return true // no OAuth needed
	}
	tokenFile := cfg.OAuth.TokenFile
	if tokenFile == "" {
		tokenFile = "~/.config/mcpagent/tokens/default.json"
	}
	if strings.HasPrefix(tokenFile, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			tokenFile = filepath.Join(home, tokenFile[2:])
		}
	}
	_, err := os.Stat(tokenFile)
	return err == nil
}

// initializeToolCache initializes the tool cache on server startup using existing mcpcache service
func (api *StreamingAPI) initializeToolCache() {
	api.logger.Info("🚀 Initializing tool cache on server startup using existing mcpcache service...")

	// Get the existing cache manager
	cacheManager := mcpcache.GetCacheManager(api.logger)

	// Load previously failed servers from disk to avoid re-attempting OAuth failures
	api.loadDiscoveryFailedServers()

	// Log cache statistics
	stats := cacheManager.GetStats()
	api.logger.Info(fmt.Sprintf("📊 Cache stats: total_entries=%v, valid_entries=%v, cache_dir=%v",
		stats["total_entries"], stats["valid_entries"], stats["cache_directory"]))

	// Load merged config (base + user additions)
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to load merged config: %v", err), err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath, api.logger)
		cfg = api.mcpConfig
	}

	api.logger.Info(fmt.Sprintf("📋 Loaded %d servers from config", len(cfg.MCPServers)))

	cachedServers := 0
	missedServers := 0
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
			api.logger.Debug(fmt.Sprintf("✅ Cache HIT for server %s (tools=%d)", serverName, len(entry.Tools)))
			// Convert cached entry to ToolStatus
			toolStatus := api.convertCacheEntryToToolStatus(entry)
			api.toolStatusMux.Lock()
			api.toolStatus[serverName] = toolStatus
			api.toolStatusMux.Unlock()
		} else {
			missedServers++
			// Truncate cache key for logging
			truncatedKey := cacheKey
			if len(cacheKey) > 50 {
				truncatedKey = cacheKey[:50] + "..."
			}
			api.logger.Debug(fmt.Sprintf("❌ Cache MISS for server %s (key=%s)", serverName, truncatedKey))
		}
	}

	api.logger.Info(fmt.Sprintf("📊 Cache lookup results: %d hits, %d misses out of %d total servers",
		cachedServers, missedServers, len(cfg.MCPServers)))

	if cachedServers > 0 {
		api.logger.Info(fmt.Sprintf("✅ Loaded %d servers from existing mcpcache", cachedServers))
	}

	// Check if we need to discover more servers
	totalServers := len(cfg.MCPServers)
	if cachedServers < totalServers {
		missingServers := totalServers - cachedServers
		api.logger.Info(fmt.Sprintf("🔄 Found %d cached servers, but config has %d servers. Starting background discovery for %d missing servers...",
			cachedServers, totalServers, missingServers))
		api.startBackgroundDiscovery()
	} else {
		api.logger.Info(fmt.Sprintf("✅ All %d servers found in cache, starting periodic refresh only", cachedServers))
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
	// Get cache manager to use configured TTL
	cacheManager := mcpcache.GetCacheManager(api.logger)
	ttlMinutes := cacheManager.GetTTL()

	// Convert ToolDetail to llmtypes.Tool format using the centralized conversion function
	llmTools, err := mcpclient.ToolDetailsAsLLM(toolStatus.Tools)
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to convert tool details to LLM tools: %v", err), err)
		// Return empty cache entry on error
		return &mcpcache.CacheEntry{
			ServerName:   serverName,
			Tools:        []llmtypes.Tool{},
			Prompts:      []mcp.Prompt{},
			Resources:    []mcp.Resource{},
			SystemPrompt: "",
			CreatedAt:    time.Now(),
			TTLMinutes:   ttlMinutes,
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
		TTLMinutes:   ttlMinutes, // Use configured TTL from cache manager
		Protocol:     "unknown",  // Will be updated by actual discovery
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

	api.logger.Info("🔄 Starting background tool discovery using mcpcache service...")

	// Get cache manager
	cacheManager := mcpcache.GetCacheManager(api.logger)

	// Load merged config (base + user additions)
	cfg, err := api.loadMergedConfig()
	if err != nil {
		api.logger.Error(fmt.Sprintf("Failed to load merged config: %v", err), err)
		// Fallback to base config only
		api.mcpConfig.ReloadConfig(api.mcpConfigPath, api.logger)
		cfg = api.mcpConfig
	}

	// Cleanup: Remove in-memory status for servers that no longer exist in config
	api.toolStatusMux.Lock()
	for name := range api.toolStatus {
		if _, exists := cfg.MCPServers[name]; !exists {
			delete(api.toolStatus, name)
			api.logger.Info(fmt.Sprintf("🗑️ Removed deleted server from status: %s", name))
		}
	}
	api.toolStatusMux.Unlock()

	discoveredServers := 0
	for serverName := range cfg.MCPServers {
		// Skip servers that previously failed with permanent errors (auth, unauthorized)
		// These won't recover without config changes, so don't waste time retrying
		if reason, failed := api.discoveryFailedServers[serverName]; failed {
			api.logger.Debug(fmt.Sprintf("⏭️ Skipping previously failed server %s: %s", serverName, reason))
			continue
		}

		// Get server configuration for cache key generation
		serverConfig, err := cfg.GetServer(serverName)
		if err != nil {
			api.logger.Warn(fmt.Sprintf("⚠️ Server %s not found in config, skipping: %v", serverName, err))
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

		// Pre-check: skip OAuth servers with no token file — avoids ~19s of futile retries
		if !hasOAuthTokenFile(serverConfig) {
			reason := "OAuth token file not found — authentication required before discovery"
			api.discoveryFailedServers[serverName] = reason
			api.logger.Info(fmt.Sprintf("⏭️ Skipping server %s: no OAuth token file", serverName))
			api.appendServerLog(serverName, "warn", reason)

			api.toolStatusMux.Lock()
			api.toolStatus[serverName] = ToolStatus{
				Name:   serverName,
				Server: serverName,
				Status: "error",
				Error:  "OAuth authentication required — no token available",
			}
			api.toolStatusMux.Unlock()
			continue
		}

		// No valid cache, discover fresh data
		api.logger.Info(fmt.Sprintf("🔍 Discovering tools for server: %s", serverName))
		api.appendServerLog(serverName, "info", "Starting background discovery...")

		// Each server gets its own 5-minute timeout so one slow server
		// cannot starve subsequent servers of connection time.
		serverCtx, serverCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		result, err := api.discoverServerToolsDetailed(serverCtx, serverName)
		serverCancel()
		if err != nil {
			errMsg := err.Error()
			api.appendServerLog(serverName, "error", fmt.Sprintf("Discovery failed: %v", err))
			api.logger.Warn(fmt.Sprintf("⚠️ Failed to discover tools for server %s: %v", serverName, err))

			// Mark servers with permanent errors so they're not retried on subsequent cycles.
			// Auth/unauthorized errors won't resolve without config changes.
			if strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "401") ||
				strings.Contains(errMsg, "OAuth") || strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "403") {
				api.discoveryFailedServers[serverName] = errMsg
				api.logger.Info(fmt.Sprintf("🚫 Server %s marked as permanently failed (auth error), will not retry", serverName))
			}

			// Store the error in toolStatus so the UI shows the failure
			api.toolStatusMux.Lock()
			api.toolStatus[serverName] = ToolStatus{
				Name:   serverName,
				Server: serverName,
				Status: "error",
				Error:  errMsg,
			}
			api.toolStatusMux.Unlock()
			continue
		}

		// Check if discovery returned an error status (e.g. OAuth required, auth failed).
		// discoverServerToolsDetailed returns (toolStatus, nil) for these — the error
		// is in result.Status/result.Error, not in the Go error return value.
		if result.Status == "error" {
			errMsg := result.Error
			api.appendServerLog(serverName, "error", fmt.Sprintf("Discovery returned error status: %s", errMsg))
			api.logger.Warn(fmt.Sprintf("⚠️ Server %s discovery returned error: %s", serverName, errMsg))

			if result.RequiresOAuth ||
				strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "401") ||
				strings.Contains(errMsg, "OAuth") || strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "403") {
				api.discoveryFailedServers[serverName] = errMsg
				api.logger.Info(fmt.Sprintf("🚫 Server %s marked as permanently failed (auth error), will not retry", serverName))
			}

			// Store the error in toolStatus so the UI shows the failure
			api.toolStatusMux.Lock()
			api.toolStatus[serverName] = *result
			api.toolStatusMux.Unlock()
			continue
		}

		// Convert ToolStatus to CacheEntry and write to mcpcache
		cacheEntry := api.convertToolStatusToCacheEntry(result, serverName)
		if err := cacheManager.Put(cacheEntry, serverConfig); err != nil {
			api.logger.Warn(fmt.Sprintf("⚠️ Failed to write cache for server %s: %v", serverName, err))
		} else {
			api.logger.Info(fmt.Sprintf("💾 Cached tools for server: %s (%d tools)", serverName, len(result.Tools)))
		}

		// Update in-memory cache for immediate API responses
		api.toolStatusMux.Lock()
		api.toolStatus[serverName] = *result
		api.toolStatusMux.Unlock()

		discoveredServers++
	}

	api.lastDiscovery = time.Now()
	api.logger.Info(fmt.Sprintf("✅ Background tool discovery completed: %d servers processed", discoveredServers))

	// Persist failed servers to disk so they're skipped on next restart
	if len(api.discoveryFailedServers) > 0 {
		api.persistDiscoveryFailedServers()
	}

	// Start periodic refresh (every 24 hours)
	api.startPeriodicRefresh()
}

// startPeriodicRefresh starts periodic background refresh
func (api *StreamingAPI) startPeriodicRefresh() {
	api.discoveryMux.Lock()
	defer api.discoveryMux.Unlock()

	if api.discoveryTicker != nil {
		return // Already started
	}

	api.discoveryTicker = time.NewTicker(24 * time.Hour)
	go func() {
		for range api.discoveryTicker.C {
			api.logger.Info("🔄 Starting periodic tool discovery refresh...")
			api.runBackgroundDiscovery()
		}
	}()

	api.logger.Info("⏰ Started periodic tool discovery refresh (every 24 hours)")
}

// stopPeriodicRefresh stops the periodic refresh
func (api *StreamingAPI) stopPeriodicRefresh() {
	api.discoveryMux.Lock()
	defer api.discoveryMux.Unlock()

	if api.discoveryTicker != nil {
		api.discoveryTicker.Stop()
		api.discoveryTicker = nil
		api.logger.Info("⏹️ Stopped periodic tool discovery refresh")
	}
}

// getToolStatusForUser returns tool status with user-specific OAuth status
// Optimized: Only checks token file existence (fast) - avoids config reload and HTTP requests
func (api *StreamingAPI) getToolStatusForUser(status ToolStatus, userID string) ToolStatus {
	// If the status already shows OAuth is required, check if this user has authenticated
	if status.RequiresOAuth {
		// Fast path: just check if token file exists
		userTokenFile := getUserTokenFilePath(userID, status.Server)
		expandedPath := expandPath(userTokenFile)
		if _, err := os.Stat(expandedPath); err == nil {
			// User has authenticated - clear the OAuth required flag
			status.RequiresOAuth = false
			status.OAuthEndpoints = nil
			status.Error = ""
			// Note: The tools may still be empty if discovery failed for other reasons
		}
	}
	return status
}

// extractMCPRemoteURL extracts the remote server URL from mcp-remote args.
// mcp-remote is commonly used to proxy remote HTTP MCP servers via stdio:
//
//	command: "npx", args: ["mcp-remote", "https://example.com/mcp"]
func extractMCPRemoteURL(command string, args []string) string {
	if len(args) < 2 {
		return ""
	}
	// Check for npx/npx.cmd mcp-remote pattern or direct mcp-remote command
	isMCPRemote := false
	urlArgIdx := -1
	if (command == "npx" || command == "npx.cmd") && args[0] == "mcp-remote" {
		isMCPRemote = true
		urlArgIdx = 1
	} else if command == "mcp-remote" {
		isMCPRemote = true
		urlArgIdx = 0
	}
	if !isMCPRemote || urlArgIdx >= len(args) {
		return ""
	}
	url := args[urlArgIdx]
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		return url
	}
	return ""
}

// tryOAuthDiscovery attempts to discover OAuth endpoints from a 401 response
func (api *StreamingAPI) tryOAuthDiscovery(ctx context.Context, serverURL string) *OAuthEndpoints {
	// Try RFC 8414 well-known discovery first (more reliable)
	if endpoints, err := oauth.DiscoverFromWellKnown(serverURL); err == nil {
		api.logger.Debug(fmt.Sprintf("Discovered OAuth via RFC 8414 well-known: auth=%s, token=%s",
			endpoints.AuthURL, endpoints.TokenURL))
		return &OAuthEndpoints{
			AuthURL:  endpoints.AuthURL,
			TokenURL: endpoints.TokenURL,
		}
	}

	// Fallback: Try 401 response header discovery
	resp, err := http.Get(serverURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	// Only proceed if we got 401 Unauthorized
	if resp.StatusCode != http.StatusUnauthorized {
		return nil
	}

	// Try to extract OAuth endpoints from response headers
	authHeader := resp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		return nil
	}

	// Use the oauth package's discovery logic
	endpoints, err := oauth.DiscoverFromResponse(resp)
	if err != nil {
		api.logger.Debug(fmt.Sprintf("Failed to discover OAuth endpoints: %v", err))
		return nil
	}

	return &OAuthEndpoints{
		AuthURL:  endpoints.AuthURL,
		TokenURL: endpoints.TokenURL,
	}
}

// --- MCP/CUSTOM/VIRTUAL EXECUTION APIs MOVED TO mcpagent/executor ---
// These handlers are now provided by the mcpagent/executor library.
// See: mcpagent/executor/handlers.go
// Routes are wired in server.go using executor.NewExecutorHandlers()
