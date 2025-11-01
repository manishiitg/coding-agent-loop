package mcpclient

import (
	"context"
	"fmt"
	"sync"

	"mcp-agent/agent_go/internal/utils"

	"github.com/mark3labs/mcp-go/client"
)

// StdioManager provides stdio connection management with pooling
type StdioManager struct {
	command   string
	args      []string
	env       []string
	logger    utils.ExtendedLogger
	pool      *StdioConnectionPool
	serverKey string
}

// Global connection pool for stdio connections
var (
	globalStdioPool *StdioConnectionPool
	poolOnce        sync.Once
)

// NewStdioManager creates a new stdio manager with our ExtendedLogger interface
func NewStdioManager(command string, args []string, env []string, logger utils.ExtendedLogger) *StdioManager {
	logger.Infof("🔧 [STDIO DEBUG] Creating StdioManager with command: %s, args: %v", command, args)

	// Initialize global pool if not already done
	poolOnce.Do(func() {
		globalStdioPool = NewStdioConnectionPool(10, logger) // Max 10 connections
		logger.Infof("🔧 [STDIO POOL] Global stdio connection pool initialized")
	})

	// Create server key for this configuration
	serverKey := fmt.Sprintf("%s_%v", command, args)

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
	s.logger.Warnf("⚠️ [STDIO DEBUG] CreateClient is deprecated, use Connect() instead for connection pooling")

	// Skip the NPX test to avoid large output buffer issues
	// The testNPXCommand function uses bufio.Scanner which has buffer limitations
	// and can cause "token too long" errors with large browser outputs
	s.logger.Infof("🔧 [STDIO DEBUG] Skipping NPX test to avoid large output buffer issues")

	// Use NewStdioMCPClient which auto-starts the connection
	mcpClient, err := client.NewStdioMCPClient(s.command, s.env, s.args...)
	if err != nil {
		s.logger.Errorf("❌ [STDIO DEBUG] Failed to create stdio client: %w", err)
		return nil, fmt.Errorf("failed to create stdio client: %w", err)
	}
	s.logger.Infof("✅ [STDIO DEBUG] Stdio client created successfully")

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
	s.logger.Infof("🔧 [STDIO DEBUG] Starting stdio connection process with pooling...")

	// Use connection pool to get or create a connection
	mcpClient, err := s.pool.GetConnection(ctx, s.serverKey, s.command, s.args, s.env)
	if err != nil {
		s.logger.Errorf("❌ [STDIO DEBUG] Failed to get stdio connection from pool: %w", err)
		return nil, fmt.Errorf("failed to get stdio connection from pool: %w", err)
	}

	s.logger.Infof("✅ [STDIO DEBUG] Stdio connection obtained from pool successfully")
	return mcpClient, nil
}
