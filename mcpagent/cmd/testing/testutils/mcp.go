package testutils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/spf13/viper"
)

// LoadTestMCPConfig loads an MCP configuration file for testing.
// If path is empty, it tries to get the path from viper config.
// Returns an error if the config cannot be loaded.
func LoadTestMCPConfig(path string, logger loggerv2.Logger) (*mcpclient.MCPConfig, error) {
	if path == "" {
		path = viper.GetString("config")
	}

	// Use default if still empty
	if path == "" {
		path = GetDefaultTestConfigPath()
	}

	if path == "" {
		return nil, fmt.Errorf("no MCP config path provided and no default found")
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("MCP config file does not exist: %s", path)
	}

	config, err := mcpclient.LoadConfig(path, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config from %s: %w", path, err)
	}

	if logger != nil {
		logger.Info("MCP config loaded",
			loggerv2.String("path", path),
			loggerv2.Int("server_count", len(config.MCPServers)))
	}

	return config, nil
}

// CreateTempMCPConfig creates a temporary MCP configuration file with the specified servers.
// Returns the path to the temporary file and a cleanup function.
// The caller should call the cleanup function to remove the temporary file.
func CreateTempMCPConfig(servers map[string]interface{}, logger loggerv2.Logger) (string, func(), error) {
	// Create minimal config structure
	config := map[string]interface{}{
		"mcpServers": servers,
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "mcp-test-config-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	// Write config to file
	if _, err := tmpFile.Write(jsonData); err != nil {
		_ = tmpFile.Close()    //nolint:gosec // Close errors are non-critical in cleanup
		_ = os.Remove(tmpPath) //nolint:gosec // Cleanup errors are non-critical
		return "", nil, fmt.Errorf("failed to write temp config: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:gosec // Cleanup errors are non-critical
		return "", nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Cleanup function
	cleanup := func() {
		if err := os.Remove(tmpPath); err != nil && logger != nil {
			logger.Warn("Failed to remove temp config file",
				loggerv2.String("path", tmpPath),
				loggerv2.Error(err))
		}
	}

	if logger != nil {
		logger.Debug("Created temporary MCP config",
			loggerv2.String("path", tmpPath))
	}

	return tmpPath, cleanup, nil
}

// GetDefaultTestConfigPath returns the default path for test MCP configuration.
// Checks common locations in order of preference.
func GetDefaultTestConfigPath() string {
	// Common default paths to check
	paths := []string{
		"configs/mcp_servers_clean_user.json",
		"configs/mcp_servers_simple.json",
		"../configs/mcp_servers_clean_user.json",
		"../../configs/mcp_servers_clean_user.json",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			// Resolve absolute path
			if absPath, err := filepath.Abs(path); err == nil {
				return absPath
			}
			return path
		}
	}

	return ""
}
