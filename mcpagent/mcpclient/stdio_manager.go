package mcpclient

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"

	loggerv2 "mcpagent/logger/v2"

	"github.com/mark3labs/mcp-go/client"
)

// StdioManager provides stdio connection management with pooling
type StdioManager struct {
	command   string
	args      []string
	env       []string
	logger    loggerv2.Logger
	pool      *StdioConnectionPool
	serverKey string
}

// Global connection pool for stdio connections
var (
	globalStdioPool *StdioConnectionPool
	poolOnce        sync.Once
)

// NewStdioManager creates a new stdio manager
func NewStdioManager(command string, args []string, env []string, logger loggerv2.Logger) *StdioManager {
	logger.Debug("Creating StdioManager",
		loggerv2.String("command", command),
		loggerv2.Any("args", args))

	// Initialize global pool if not already done
	poolOnce.Do(func() {
		globalStdioPool = NewStdioConnectionPool(10, logger) // Max 10 connections
		logger.Debug("Global stdio connection pool initialized")
	})

	// Create server key for this configuration
	// Include environment variables in the key to ensure connections with different env vars are not reused
	envHash := hashEnvVars(env)
	serverKey := fmt.Sprintf("%s_%v_%s", command, args, envHash)

	return &StdioManager{
		command:   command,
		args:      args,
		env:       env,
		logger:    logger,
		pool:      globalStdioPool,
		serverKey: serverKey,
	}
}

// CreateClient creates a new stdio client with direct connection (DEPRECATED - use Connect instead)
// This method is kept for backward compatibility but should not be used in new code
func (s *StdioManager) CreateClient() (*client.Client, error) {
	s.logger.Warn("CreateClient is deprecated, use Connect() instead for connection pooling")

	// Skip the NPX test to avoid large output buffer issues
	// The testNPXCommand function uses bufio.Scanner which has buffer limitations
	// and can cause "token too long" errors with large browser outputs
	s.logger.Debug("Skipping NPX test to avoid large output buffer issues")

	// Use NewStdioMCPClient which auto-starts the connection
	mcpClient, err := client.NewStdioMCPClient(s.command, s.env, s.args...)
	if err != nil {
		s.logger.Error("Failed to create stdio client", err)
		return nil, fmt.Errorf("failed to create stdio client: %w", err)
	}
	s.logger.Debug("Stdio client created successfully")

	return mcpClient, nil
}

// GetPoolStats returns statistics about the connection pool
func (s *StdioManager) GetPoolStats() map[string]interface{} {
	return s.pool.GetPoolStats()
}

// CloseConnection closes the connection for this server
func (s *StdioManager) CloseConnection() {
	s.pool.CloseConnection(s.serverKey)
}

// CloseAllConnections closes all connections in the pool
func (s *StdioManager) CloseAllConnections() {
	s.pool.CloseAllConnections()
}

// GetGlobalPoolStats returns statistics about the global connection pool
func GetGlobalPoolStats() map[string]interface{} {
	if globalStdioPool == nil {
		return map[string]interface{}{
			"error": "Global stdio pool not initialized",
		}
	}
	return globalStdioPool.GetPoolStats()
}

// StopGlobalPool stops the global connection pool
func StopGlobalPool() {
	if globalStdioPool != nil {
		globalStdioPool.Stop()
	}
}

// Connect creates and starts a stdio client with connection pooling
func (s *StdioManager) Connect(ctx context.Context) (*client.Client, error) {
	s.logger.Debug("Starting stdio connection process with pooling")

	// Use connection pool to get or create a connection
	mcpClient, err := s.pool.GetConnection(ctx, s.serverKey, s.command, s.args, s.env)
	if err != nil {
		s.logger.Error("Failed to get stdio connection from pool", err)
		return nil, fmt.Errorf("failed to get stdio connection from pool: %w", err)
	}

	s.logger.Debug("Stdio connection obtained from pool successfully")
	return mcpClient, nil
}

// hashEnvVars creates a deterministic hash of environment variables
// This ensures that connections with different env vars get different server keys
func hashEnvVars(env []string) string {
	if len(env) == 0 {
		return "noenv"
	}

	// Sort env vars to ensure deterministic hash
	sorted := make([]string, len(env))
	copy(sorted, env)
	sort.Strings(sorted)

	// Create hash of sorted env vars
	hasher := sha256.New()
	for _, e := range sorted {
		hasher.Write([]byte(e))
		hasher.Write([]byte("\n"))
	}
	hash := hasher.Sum(nil)

	// Return first 16 characters of hex hash (enough for uniqueness)
	return fmt.Sprintf("%x", hash)[:16]
}
