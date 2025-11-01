package mcpcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpclient"

	"mcp-agent/agent_go/internal/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
)

// CacheEntry represents a cached MCP server connection and its metadata
type CacheEntry struct {
	// Server identification
	ServerName string `json:"server_name"`

	// Connection data
	Tools        []llmtypes.Tool `json:"tools"`
	Prompts      []mcp.Prompt    `json:"prompts"`
	Resources    []mcp.Resource  `json:"resources"`
	SystemPrompt string          `json:"system_prompt"`

	// Metadata
	CreatedAt    time.Time              `json:"created_at"`
	LastAccessed time.Time              `json:"last_accessed"`
	TTLMinutes   int                    `json:"ttl_minutes"`
	Protocol     string                 `json:"protocol"`
	ServerInfo   map[string]interface{} `json:"server_info,omitempty"`

	// Cache management
	IsValid      bool   `json:"is_valid"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// IsExpired checks if the cache entry has expired
func (ce *CacheEntry) IsExpired() bool {
	if !ce.IsValid {
		return true
	}
	expirationTime := ce.CreatedAt.Add(time.Duration(ce.TTLMinutes) * time.Minute)
	return time.Now().After(expirationTime)
}

// UpdateAccessTime updates the last accessed timestamp
func (ce *CacheEntry) UpdateAccessTime() {
	ce.LastAccessed = time.Now()
}

// CacheManager manages MCP server connection caching
type CacheManager struct {
	cacheDir   string
	ttlMinutes int
	logger     utils.ExtendedLogger
	mu         sync.RWMutex
	cache      map[string]*CacheEntry // cache key -> entry
}

// Singleton instance
var (
	instance *CacheManager
	once     sync.Once
)

// GetCacheManager returns the singleton cache manager instance
func GetCacheManager(logger utils.ExtendedLogger) *CacheManager {
	once.Do(func() {
		// Use environment variable if set, otherwise default to agent_go/cache
		cacheDir := os.Getenv("MCP_CACHE_DIR")
		if cacheDir == "" {
			// Default to agent_go/cache directory (works for both local and Docker)
			cacheDir = "/app/cache" // Docker mount point
			if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
				// For local development, use relative path to agent_go/cache
				cacheDir = filepath.Join(".", "cache")
			}
		}
		// Get TTL from environment variable, default to 7 days (10080 minutes)
		ttlMinutes := 10080 // 7 days default
		if ttlEnv := os.Getenv("MCP_CACHE_TTL_MINUTES"); ttlEnv != "" {
			if parsedTTL, err := strconv.Atoi(ttlEnv); err == nil && parsedTTL > 0 {
				ttlMinutes = parsedTTL
			} else if logger != nil {
				logger.Warnf("Invalid MCP_CACHE_TTL_MINUTES value '%s', using default %d minutes", ttlEnv, ttlMinutes)
			}
		}

		instance = &CacheManager{
			cacheDir:   cacheDir,
			ttlMinutes: ttlMinutes, // Configurable TTL via environment variable
			logger:     logger,
			cache:      make(map[string]*CacheEntry),
		}

		// Initialize cache directory
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			if logger != nil {
				logger.Warnf("Failed to create cache directory %s: %v", cacheDir, err)
			}
		}

		// Load existing cache entries
		instance.loadExistingCache()
	})
	return instance
}

// GenerateServerConfigHash creates a hash of the server configuration
// This includes command, args, env vars, URL, headers, and protocol
func GenerateServerConfigHash(config mcpclient.MCPServerConfig) string {
	// Create a deterministic representation of the config
	configData := struct {
		Command  string            `json:"command"`
		Args     []string          `json:"args"`
		Env      map[string]string `json:"env"`
		URL      string            `json:"url"`
		Headers  map[string]string `json:"headers"`
		Protocol string            `json:"protocol"`
	}{
		Command:  config.Command,
		Args:     config.Args,
		Env:      config.Env,
		URL:      config.URL,
		Headers:  config.Headers,
		Protocol: string(config.Protocol),
	}

	// Sort maps for deterministic output
	if configData.Env != nil {
		sortedEnv := make(map[string]string)
		var envKeys []string
		for k := range configData.Env {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		for _, k := range envKeys {
			sortedEnv[k] = configData.Env[k]
		}
		configData.Env = sortedEnv
	}

	if configData.Headers != nil {
		sortedHeaders := make(map[string]string)
		var headerKeys []string
		for k := range configData.Headers {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)
		for _, k := range headerKeys {
			sortedHeaders[k] = configData.Headers[k]
		}
		configData.Headers = sortedHeaders
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(configData)
	if err != nil {
		// Fallback to simple string representation
		return fmt.Sprintf("config_%s", config.Command)
	}

	// Generate SHA256 hash
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:]) // Use full hash to prevent collisions
}

// GenerateUnifiedCacheKey creates a cache key using server name and configuration hash
// This ensures cache is invalidated when server configuration changes
func GenerateUnifiedCacheKey(serverName string, config mcpclient.MCPServerConfig) string {
	// Clean server name
	cleanServerName := strings.TrimSpace(serverName)

	// Generate configuration hash
	configHash := GenerateServerConfigHash(config)

	// Combine server name and config hash
	return fmt.Sprintf("unified_%s_%s", cleanServerName, configHash)
}

// Get retrieves a cache entry if it exists and is valid
func (cm *CacheManager) Get(cacheKey string) (*CacheEntry, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	entry, exists := cm.cache[cacheKey]
	if !exists {
		return nil, false
	}

	// Check if entry is expired
	if entry.IsExpired() {
		age := time.Since(entry.CreatedAt)
		ttl := time.Duration(entry.TTLMinutes) * time.Minute
		cm.logger.Debugf("Cache entry expired for key: %s", cacheKey)

		// Note: We don't emit expired events here as we don't have tracers available
		// The expiration event would be emitted when the entry is actually cleaned up
		_ = age // Prevent unused variable warning
		_ = ttl // Prevent unused variable warning

		return nil, false
	}

	// Update access time
	entry.UpdateAccessTime()

	cm.logger.Debugf("Cache hit for key: %s", cacheKey)
	return entry, true
}

// Put stores a cache entry using configuration-aware cache key
func (cm *CacheManager) Put(entry *CacheEntry, config mcpclient.MCPServerConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Use configuration-aware cache key
	cacheKey := GenerateUnifiedCacheKey(entry.ServerName, config)
	entry.UpdateAccessTime()

	// Store in memory cache
	cm.cache[cacheKey] = entry

	// Persist to file using configuration-aware naming
	return cm.saveToFile(entry, config)
}

// Invalidate removes a cache entry
func (cm *CacheManager) Invalidate(cacheKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.cache, cacheKey)

	// Remove from filesystem
	cacheFile := cm.getCacheFilePath(cacheKey)
	if err := os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file %s: %w", cacheFile, err)
	}

	cm.logger.Debugf("Invalidated cache entry: %s", cacheKey)
	return nil
}

// InvalidateByServer invalidates all cache entries for a specific server
func (cm *CacheManager) InvalidateByServer(configPath, serverName string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var keysToRemove []string

	// Find all keys for this server
	for key, entry := range cm.cache {
		if entry.ServerName == serverName {
			keysToRemove = append(keysToRemove, key)
		}
	}

	// Remove entries
	for _, key := range keysToRemove {
		delete(cm.cache, key)

		// Remove from filesystem
		cacheFile := cm.getCacheFilePath(key)
		if err := os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
			cm.logger.Warnf("Failed to remove cache file %s: %v", cacheFile, err)
		}
	}

	if len(keysToRemove) > 0 {
		cm.logger.Infof("Invalidated %d cache entries for server %s", len(keysToRemove), serverName)
	}

	return nil
}

// GetAllEntries returns all cached entries (for debugging and registry integration)
func (cm *CacheManager) GetAllEntries() map[string]*CacheEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy of the cache map
	result := make(map[string]*CacheEntry)
	for key, entry := range cm.cache {
		result[key] = entry
	}
	return result
}

// Clear removes all cache entries
func (cm *CacheManager) Clear() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Clear memory cache
	cm.cache = make(map[string]*CacheEntry)

	// Remove all cache files
	return cm.clearCacheDirectory()
}

// GetStats returns cache statistics
func (cm *CacheManager) GetStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	totalEntries := len(cm.cache)
	validEntries := 0
	expiredEntries := 0
	totalSize := int64(0)

	for _, entry := range cm.cache {
		if entry.IsValid && !entry.IsExpired() {
			validEntries++
		} else {
			expiredEntries++
		}

		// Estimate size (rough calculation)
		entrySize := len(entry.ServerName) + len(entry.SystemPrompt)
		for _, tool := range entry.Tools {
			entrySize += len(tool.Function.Name) + len(tool.Function.Description)
		}
		totalSize += int64(entrySize)
	}

	return map[string]interface{}{
		"total_entries":   totalEntries,
		"valid_entries":   validEntries,
		"expired_entries": expiredEntries,
		"estimated_size":  totalSize,
		"cache_directory": cm.cacheDir,
		"ttl_minutes":     cm.ttlMinutes,
	}
}

// Cleanup removes expired entries from both memory and filesystem
func (cm *CacheManager) Cleanup() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var expiredKeys []string

	// Find expired entries
	for key, entry := range cm.cache {
		if entry.IsExpired() {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// Remove expired entries
	for _, key := range expiredKeys {
		delete(cm.cache, key)

		// Remove from filesystem
		cacheFile := cm.getCacheFilePath(key)
		if err := os.Remove(cacheFile); err != nil && !os.IsNotExist(err) {
			cm.logger.Warnf("Failed to remove expired cache file %s: %v", cacheFile, err)
		}
	}

	if len(expiredKeys) > 0 {
		cm.logger.Infof("Cleaned up %d expired cache entries", len(expiredKeys))
	}

	return nil
}

// loadExistingCache loads cache entries from the filesystem
func (cm *CacheManager) loadExistingCache() {
	files, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		if cm.logger != nil {
			cm.logger.Debugf("Cache directory does not exist or cannot be read: %w", err)
		}
		return
	}

	loadedCount := 0

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") {
			cacheFile := filepath.Join(cm.cacheDir, file.Name())
			if entry := cm.loadFromFile(cacheFile); entry != nil {
				// Use filename as cache key (config-aware format)
				fileName := strings.TrimSuffix(file.Name(), ".json")
				cm.cache[fileName] = entry
				loadedCount++
				if cm.logger != nil {
					cm.logger.Debugf("Loaded cache entry: %s", fileName)
				}
			}
		}
	}

	if loadedCount > 0 && cm.logger != nil {
		cm.logger.Infof("Loaded %d cache entries from filesystem", loadedCount)
	}
}

// saveToFile persists a cache entry to the filesystem using configuration-aware naming
func (cm *CacheManager) saveToFile(entry *CacheEntry, config mcpclient.MCPServerConfig) error {
	// Use configuration-aware cache key for file naming
	cacheFile := cm.getCacheFilePath(GenerateUnifiedCacheKey(entry.ServerName, config))

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	// Write to file
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	cm.logger.Debugf("Saved cache entry to file: %s", cacheFile)
	return nil
}

// loadFromFile loads a cache entry from the filesystem
func (cm *CacheManager) loadFromFile(cacheFile string) *CacheEntry {
	//nolint:gosec // G304: cacheFile path is generated internally from validated inputs
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		if cm.logger != nil {
			cm.logger.Debugf("Failed to read cache file %s: %v", cacheFile, err)
		}
		return nil
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		if cm.logger != nil {
			cm.logger.Warnf("Failed to unmarshal cache file %s: %v", cacheFile, err)
		}
		return nil
	}

	// Check if entry is still valid
	if entry.IsExpired() {
		cm.logger.Debugf("Loaded expired cache entry: %s", cacheFile)
		// Don't return expired entries
		os.Remove(cacheFile) // Clean up expired file
		return nil
	}

	return &entry
}

// ReloadFromDisk reloads a specific cache entry from disk and updates the in-memory cache
func (cm *CacheManager) ReloadFromDisk(cacheKey string) *CacheEntry {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cacheFile := cm.getCacheFilePath(cacheKey)

	// Check if file exists
	if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
		if cm.logger != nil {
			cm.logger.Debugf("Cache file does not exist: %s", cacheFile)
		}
		return nil
	}

	// Load the entry from disk
	entry := cm.loadFromFile(cacheFile)
	if entry == nil {
		if cm.logger != nil {
			cm.logger.Debugf("Failed to load cache entry from disk: %s", cacheFile)
		}
		return nil
	}

	// Update the in-memory cache
	cm.cache[cacheKey] = entry

	if cm.logger != nil {
		cm.logger.Debugf("Reloaded cache entry from disk: %s (tools: %d)", cacheKey, len(entry.Tools))
	}

	return entry
}

// getCacheFilePath returns the filesystem path for a cache key
func (cm *CacheManager) getCacheFilePath(cacheKey string) string {
	return filepath.Join(cm.cacheDir, fmt.Sprintf("%s.json", cacheKey))
}

// clearCacheDirectory removes all files from the cache directory
func (cm *CacheManager) clearCacheDirectory() error {
	files, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		filePath := filepath.Join(cm.cacheDir, file.Name())
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			cm.logger.Warnf("Failed to remove cache file %s: %v", filePath, err)
		}
	}

	return nil
}

// GetCacheDirectory returns the cache directory path
func (cm *CacheManager) GetCacheDirectory() string {
	return cm.cacheDir
}

// SetTTL sets the TTL for cache entries (in minutes)
func (cm *CacheManager) SetTTL(minutes int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.ttlMinutes = minutes
}

// GetTTL returns the current TTL setting
func (cm *CacheManager) GetTTL() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.ttlMinutes
}
