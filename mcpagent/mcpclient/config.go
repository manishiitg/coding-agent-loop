package mcpclient

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/oauth"
)

// ProtocolType defines the connection protocol
type ProtocolType string

const (
	ProtocolStdio ProtocolType = "stdio"
	ProtocolSSE   ProtocolType = "sse"
	ProtocolHTTP  ProtocolType = "http"
)

// Special server name constants
const (
	// AllServers indicates that all configured MCP servers should be connected
	// This is the default behavior when no specific server is requested
	AllServers = "all"

	// NoServers indicates that no MCP servers should be connected
	// This is used when an agent should work with pure LLM reasoning without any tools
	NoServers = "NO_SERVERS"
)

// PoolConfig defines connection pooling settings
type PoolConfig struct {
	MaxConnections       int           `json:"max_connections"`
	MinConnections       int           `json:"min_connections"`
	MaxIdleTime          time.Duration `json:"max_idle_time"`
	HealthCheckInterval  time.Duration `json:"health_check_interval"`
	ConnectionTimeout    time.Duration `json:"connection_timeout"`
	ReconnectDelay       time.Duration `json:"reconnect_delay"`
	MaxReconnectAttempts int           `json:"max_reconnect_attempts"`
}

// DefaultPoolConfig returns sensible default pooling configuration
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConnections:       20,
		MinConnections:       2,
		MaxIdleTime:          15 * time.Minute, // Increased from 5 to 15 minutes
		HealthCheckInterval:  2 * time.Minute,  // Increased from 30s to 2 minutes
		ConnectionTimeout:    15 * time.Minute, // Increased from 10 minutes to 15 minutes for very slow npx commands
		ReconnectDelay:       2 * time.Second,
		MaxReconnectAttempts: 3,
	}
}

// ServerConfig represents a single MCP server configuration
type ServerConfig struct {
	Name        string            `json:"name"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	Protocol    ProtocolType      `json:"protocol"`
	PoolConfig  PoolConfig        `json:"pool_config"`
	// SSE/HTTP specific fields
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// NewServerConfig creates a new server configuration with defaults
func NewServerConfig(name string, protocol ProtocolType) ServerConfig {
	return ServerConfig{
		Name:       name,
		Protocol:   protocol,
		PoolConfig: DefaultPoolConfig(),
		Headers:    make(map[string]string),
		Env:        make(map[string]string),
	}
}

type MCPServerConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	Protocol    ProtocolType      `json:"protocol,omitempty"`
	PoolConfig  *PoolConfig       `json:"pool_config,omitempty"`
	// SSE/HTTP specific fields
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// OAuth configuration
	OAuth *oauth.OAuthConfig `json:"oauth,omitempty"`
}

// RuntimeConfigOverride allows runtime modification of MCP server configuration
// This is useful for passing workflow-specific settings like output directories
type RuntimeConfigOverride struct {
	// ArgsReplace replaces specific arg values by flag name
	// e.g., {"--output-dir": "/new/path"} will find "--output-dir" and replace the next arg
	ArgsReplace map[string]string `json:"args_replace,omitempty"`
	// ArgsAppend appends additional args to the command
	ArgsAppend []string `json:"args_append,omitempty"`
	// EnvOverride adds or overrides environment variables
	EnvOverride map[string]string `json:"env_override,omitempty"`
}

// RuntimeOverrides maps server names to their runtime configuration overrides
type RuntimeOverrides map[string]RuntimeConfigOverride

// ApplyOverride applies a RuntimeConfigOverride to the MCPServerConfig
// Returns a new MCPServerConfig with the overrides applied (does not modify original)
func (c MCPServerConfig) ApplyOverride(override RuntimeConfigOverride) MCPServerConfig {
	// Create a copy of the config
	newConfig := c
	newConfig.Args = make([]string, len(c.Args))
	copy(newConfig.Args, c.Args)

	if c.Env != nil {
		newConfig.Env = make(map[string]string, len(c.Env))
		for k, v := range c.Env {
			newConfig.Env[k] = v
		}
	}

	// Apply ArgsReplace - find flag and replace its value
	for flag, newValue := range override.ArgsReplace {
		for i := 0; i < len(newConfig.Args); i++ {
			if newConfig.Args[i] == flag && i+1 < len(newConfig.Args) {
				// Replace the value after the flag
				newConfig.Args[i+1] = newValue
				break
			}
			// Also handle --flag=value format
			if strings.HasPrefix(newConfig.Args[i], flag+"=") {
				newConfig.Args[i] = flag + "=" + newValue
				break
			}
		}
	}

	// Apply ArgsAppend
	if len(override.ArgsAppend) > 0 {
		newConfig.Args = append(newConfig.Args, override.ArgsAppend...)
	}

	// Apply EnvOverride
	if len(override.EnvOverride) > 0 {
		if newConfig.Env == nil {
			newConfig.Env = make(map[string]string)
		}
		for k, v := range override.EnvOverride {
			newConfig.Env[k] = v
		}
	}

	return newConfig
}

// GetPoolConfig returns the pool configuration, using defaults if not specified
func (c *MCPServerConfig) GetPoolConfig() PoolConfig {
	if c.PoolConfig != nil {
		return *c.PoolConfig
	}
	return DefaultPoolConfig()
}

// GetProtocol returns the protocol type with smart detection
func (c *MCPServerConfig) GetProtocol() ProtocolType {
	// If protocol is explicitly set, use it
	if c.Protocol != "" {
		return c.Protocol
	}

	// Smart detection based on URL
	if c.URL != "" {
		// If URL contains /sse, assume SSE protocol
		if contains(c.URL, "/sse") {
			return ProtocolSSE
		}
		// If URL starts with http:// or https://, assume HTTP
		if contains(c.URL, "http://") || contains(c.URL, "https://") {
			return ProtocolHTTP
		}
	}

	// Default to stdio
	return ProtocolStdio
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && strings.Contains(s, substr)
}

type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// LoadConfig loads MCP server configuration from the specified file
// logger is optional - if provided, debug information will be logged at debug level
// If configPath is empty, returns an empty config (useful for pure LLM mode without MCP servers)
func LoadConfig(configPath string, logger loggerv2.Logger) (*MCPConfig, error) {
	// If config path is empty, return an empty config for pure LLM mode
	if configPath == "" {
		if logger != nil {
			logger.Debug("Config path is empty, returning empty config for pure LLM mode")
		}
		return &MCPConfig{MCPServers: make(map[string]MCPServerConfig)}, nil
	}

	if logger != nil {
		logger.Debug("About to read config file", loggerv2.String("config_path", configPath))
	}
	startTime := time.Now()

	//nolint:gosec // G304: configPath comes from command-line/config, not user input
	data, err := os.ReadFile(configPath)

	readDuration := time.Since(startTime)
	if err != nil {
		if logger != nil {
			logger.Debug("os.ReadFile failed",
				loggerv2.String("config_path", configPath),
				loggerv2.Any("duration", readDuration),
				loggerv2.Error(err))
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	if logger != nil {
		logger.Debug("os.ReadFile completed",
			loggerv2.String("config_path", configPath),
			loggerv2.Any("duration", readDuration),
			loggerv2.Int("bytes_read", len(data)))
	}

	if logger != nil {
		logger.Debug("About to unmarshal JSON", loggerv2.String("config_path", configPath))
	}
	unmarshalStartTime := time.Now()

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		unmarshalDuration := time.Since(unmarshalStartTime)
		if logger != nil {
			logger.Debug("json.Unmarshal failed",
				loggerv2.String("config_path", configPath),
				loggerv2.Any("duration", unmarshalDuration),
				loggerv2.Error(err))
		}
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	unmarshalDuration := time.Since(unmarshalStartTime)
	if logger != nil {
		logger.Debug("json.Unmarshal completed",
			loggerv2.String("config_path", configPath),
			loggerv2.Any("duration", unmarshalDuration))
	}

	return &config, nil
}

// LoadMergedConfig loads the merged configuration (base + user additions)
// This mirrors the logic from mcp_config_routes.go to ensure consistency
func LoadMergedConfig(configPath string, logger loggerv2.Logger) (*MCPConfig, error) {
	userConfigPath := strings.Replace(configPath, ".json", "_user.json", 1)
	if logger != nil {
		logger.Debug("Starting LoadMergedConfig",
			loggerv2.String("base_config_path", configPath),
			loggerv2.String("user_config_path", userConfigPath))
	}
	startTime := time.Now()

	// Load base config
	if logger != nil {
		logger.Debug("About to load base config", loggerv2.String("config_path", configPath))
	}
	baseConfigStartTime := time.Now()
	baseConfig, err := LoadConfig(configPath, logger)
	baseConfigDuration := time.Since(baseConfigStartTime)
	if err != nil {
		if logger != nil {
			logger.Debug("Failed to load base config",
				loggerv2.String("config_path", configPath),
				loggerv2.Any("duration", baseConfigDuration),
				loggerv2.Error(err))
		}
		return nil, fmt.Errorf("failed to load base config: %w", err)
	}
	if logger != nil {
		logger.Debug("Base config loaded successfully",
			loggerv2.String("config_path", configPath),
			loggerv2.Any("duration", baseConfigDuration),
			loggerv2.Int("server_count", len(baseConfig.MCPServers)))
	}

	// Load user additions (if any)
	if logger != nil {
		logger.Debug("About to load user config", loggerv2.String("config_path", userConfigPath))
	}
	userConfigStartTime := time.Now()
	userConfig, err := LoadConfig(userConfigPath, logger)
	userConfigDuration := time.Since(userConfigStartTime)
	if err != nil {
		if logger != nil {
			logger.Debug("User config load failed (this is OK if file doesn't exist)",
				loggerv2.String("config_path", userConfigPath),
				loggerv2.Any("duration", userConfigDuration),
				loggerv2.Error(err))
		}
		// User config doesn't exist yet, use empty config
		userConfig = &MCPConfig{MCPServers: make(map[string]MCPServerConfig)}
	} else {
		if logger != nil {
			logger.Debug("User config loaded successfully",
				loggerv2.String("config_path", userConfigPath),
				loggerv2.Any("duration", userConfigDuration),
				loggerv2.Int("server_count", len(userConfig.MCPServers)))
		}
	}

	// Merge base config with user additions
	if logger != nil {
		logger.Debug("Starting merge operation")
	}
	mergeStartTime := time.Now()
	mergedConfig := &MCPConfig{
		MCPServers: make(map[string]MCPServerConfig),
	}

	// Add base servers first
	for name, server := range baseConfig.MCPServers {
		mergedConfig.MCPServers[name] = server
	}

	// Add user servers (these will override base servers with same name)
	for name, server := range userConfig.MCPServers {
		mergedConfig.MCPServers[name] = server
	}
	mergeDuration := time.Since(mergeStartTime)
	if logger != nil {
		logger.Debug("Merge operation completed",
			loggerv2.Any("duration", mergeDuration))
	}

	if logger != nil {
		logger.Info("Merged config",
			loggerv2.Int("base_servers", len(baseConfig.MCPServers)),
			loggerv2.Int("user_servers", len(userConfig.MCPServers)),
			loggerv2.Int("total_servers", len(mergedConfig.MCPServers)))
	}

	totalDuration := time.Since(startTime)
	if logger != nil {
		logger.Debug("LoadMergedConfig completed successfully",
			loggerv2.Any("duration", totalDuration))
	}
	return mergedConfig, nil
}

// GetServer returns the configuration for a specific server
func (c *MCPConfig) GetServer(name string) (MCPServerConfig, error) {
	server, exists := c.MCPServers[name]
	if !exists {
		return MCPServerConfig{}, fmt.Errorf("server '%s' not found in configuration", name)
	}
	return server, nil
}

// ListServers returns all configured server names
func (c *MCPConfig) ListServers() []string {
	var names []string
	for name := range c.MCPServers {
		names = append(names, name)
	}
	return names
}

// SaveConfig writes the MCPConfig to the specified file atomically
func SaveConfig(configPath string, config *MCPConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil { //nolint:gosec // 0644 permissions are intentional for user-accessible config files
		return err
	}
	return os.Rename(tmpPath, configPath)
}

// AddServer adds a new server to the config and saves it
func (c *MCPConfig) AddServer(name string, server MCPServerConfig, configPath string) error {
	c.MCPServers[name] = server
	return SaveConfig(configPath, c)
}

// EditServer edits an existing server in the config and saves it
func (c *MCPConfig) EditServer(name string, server MCPServerConfig, configPath string) error {
	c.MCPServers[name] = server
	return SaveConfig(configPath, c)
}

// RemoveServer removes a server from the config and saves it
func (c *MCPConfig) RemoveServer(name string, configPath string) error {
	delete(c.MCPServers, name)
	return SaveConfig(configPath, c)
}

// ReloadConfig reloads the config from disk
// logger is optional - if provided, debug information will be logged at debug level
func (c *MCPConfig) ReloadConfig(configPath string, logger loggerv2.Logger) error {
	newConfig, err := LoadConfig(configPath, logger)
	if err != nil {
		return err
	}
	c.MCPServers = newConfig.MCPServers
	return nil
}
