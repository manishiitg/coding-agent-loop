package mcpclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	loggerv2 "mcpagent/logger/v2"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// StdioConnection represents a pooled stdio connection
type StdioConnection struct {
	client    *client.Client
	process   *os.Process
	createdAt time.Time
	lastUsed  time.Time
	healthy   bool
	serverKey string
	mutex     sync.RWMutex
}

// StdioConnectionPool manages a pool of stdio connections
type StdioConnectionPool struct {
	connections   map[string]*StdioConnection
	mutex         sync.RWMutex
	maxSize       int
	logger        loggerv2.Logger
	cleanupTicker *time.Ticker
	cleanupDone   chan bool
}

// NewStdioConnectionPool creates a new stdio connection pool
func NewStdioConnectionPool(maxSize int, logger loggerv2.Logger) *StdioConnectionPool {
	pool := &StdioConnectionPool{
		connections: make(map[string]*StdioConnection),
		maxSize:     maxSize,
		logger:      logger,
		cleanupDone: make(chan bool),
	}

	// Start cleanup routine
	pool.startCleanupRoutine()

	return pool
}

// GetConnection retrieves or creates a stdio connection
func (p *StdioConnectionPool) GetConnection(ctx context.Context, serverKey string, command string, args []string, env []string) (*client.Client, error) {
	p.mutex.Lock()
	p.logger.Debug("Getting connection for server", loggerv2.String("server", serverKey))

	// Check if we have an existing connection
	var existingConn *StdioConnection
	var needsHealthCheck bool
	var clientToClose *client.Client
	if conn, exists := p.connections[serverKey]; exists {
		existingConn = conn
		// Quick check if already marked unhealthy
		conn.mutex.RLock()
		needsHealthCheck = conn.healthy
		conn.mutex.RUnlock()

		if !needsHealthCheck {
			// Connection already marked unhealthy, remove it
			p.logger.Info(fmt.Sprintf("❌ [STDIO POOL] Existing connection unhealthy, removing: %s", serverKey), loggerv2.String("server", serverKey))
			clientToClose = p.removeConnection(serverKey)
			existingConn = nil
		}
	}
	p.mutex.Unlock() // Unlock before any potentially long-running operations

	// Close the connection outside the mutex to avoid blocking other threads
	if clientToClose != nil {
		p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Closing unhealthy connection outside mutex: %s", serverKey), loggerv2.String("server", serverKey))
		_ = clientToClose.Close() // Ignore errors during cleanup
	}

	// If we have an existing connection, check its health (this can take time)
	if existingConn != nil && needsHealthCheck {
		if p.isConnectionHealthy(existingConn) {
			// Re-acquire lock to update lastUsed
			p.mutex.Lock()
			// Double-check connection still exists and is the same one
			if conn, stillExists := p.connections[serverKey]; stillExists && conn == existingConn {
				p.logger.Debug("Reusing existing healthy connection for server", loggerv2.String("server", serverKey))
				existingConn.mutex.Lock()
				existingConn.lastUsed = time.Now()
				existingConn.mutex.Unlock()
				p.mutex.Unlock()
				return existingConn.client, nil
			}
			p.mutex.Unlock()
		} else {
			// Connection is unhealthy, remove it
			p.mutex.Lock()
			var clientToClose *client.Client
			if _, stillExists := p.connections[serverKey]; stillExists {
				p.logger.Info(fmt.Sprintf("❌ [STDIO POOL] Existing connection unhealthy, removing: %s", serverKey), loggerv2.String("server", serverKey))
				clientToClose = p.removeConnection(serverKey)
			}
			p.mutex.Unlock()

			// Close the connection outside the mutex to avoid blocking
			if clientToClose != nil {
				p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Closing unhealthy connection outside mutex: %s", serverKey), loggerv2.String("server", serverKey))
				_ = clientToClose.Close() // Ignore errors during cleanup
			}
		}
	}

	// Create new connection WITHOUT holding the pool mutex
	// This prevents blocking other goroutines during the potentially long initialization (up to 10 minutes)
	p.logger.Debug("Creating new connection for server", loggerv2.String("server", serverKey))
	conn, err := p.createNewConnection(ctx, serverKey, command, args, env)
	if err != nil {
		return nil, fmt.Errorf("failed to create new stdio connection: %w", err)
	}

	// Re-acquire lock to add connection to pool
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Double-check another goroutine didn't create the connection while we were creating ours
	if existingConn, exists := p.connections[serverKey]; exists {
		p.logger.Debug("Another goroutine created connection, using existing one", loggerv2.String("server", serverKey))
		// Close our newly created connection and use the existing one
		if conn.client != nil {
			_ = conn.client.Close() // Ignore errors during cleanup
		}
		return existingConn.client, nil
	}

	p.connections[serverKey] = conn
	p.logger.Debug("New connection created and added to pool", loggerv2.String("server", serverKey))

	return conn.client, nil
}

// createNewConnection creates a new stdio connection
func (p *StdioConnectionPool) createNewConnection(ctx context.Context, serverKey string, command string, args []string, env []string) (*StdioConnection, error) {
	startTime := time.Now()
	p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Creating new stdio connection: %s %v", command, args), loggerv2.String("command", command), loggerv2.Any("args", args))

	// Debug: Log environment variables (but mask sensitive values)
	if p.logger != nil {
		envCount := len(env)
		envPreview := make([]string, 0)
		for _, e := range env {
			if idx := strings.IndexByte(e, '='); idx > 0 {
				key := e[:idx]
				// Always include SERVICE_ACCOUNT_PATH and DRIVE_FOLDER_ID if present
				if strings.Contains(key, "SERVICE_ACCOUNT") || strings.Contains(key, "DRIVE_FOLDER") {
					envPreview = append(envPreview, e)
				}
				// Only show first few env vars to avoid log spam
				if len(envPreview) < 10 {
					if strings.Contains(strings.ToLower(key), "secret") || strings.Contains(strings.ToLower(key), "password") || strings.Contains(strings.ToLower(key), "key") {
						envPreview = append(envPreview, fmt.Sprintf("%s=***", key))
					} else {
						envPreview = append(envPreview, e)
					}
				}
			}
		}
		if envCount > 5 {
			envPreview = append(envPreview, fmt.Sprintf("... and %d more", envCount-5))
		}
		p.logger.Debug("Environment variables",
			loggerv2.Int("total", envCount),
			loggerv2.Any("preview", envPreview))
	}

	// Create the MCP client
	p.logger.Debug("Step 1/2: Creating stdio MCP client", loggerv2.String("server", serverKey))
	clientStartTime := time.Now()
	mcpClient, err := client.NewStdioMCPClient(command, env, args...)
	if err != nil {
		clientDuration := time.Since(clientStartTime)
		p.logger.Error("Failed to create stdio client", err, loggerv2.String("duration", clientDuration.String()))
		return nil, fmt.Errorf("failed to create stdio client: %w", err)
	}
	clientDuration := time.Since(clientStartTime)
	p.logger.Debug("Stdio MCP client created successfully",
		loggerv2.String("duration", clientDuration.Round(time.Millisecond).String()))

	// Initialize the connection with timeout
	initTimeout := 10 * time.Minute
	initCtx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()

	// Start a goroutine to log progress during initialization
	progressDone := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Log every 30 seconds
		defer ticker.Stop()

		initStartTime := time.Now()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(initStartTime)
				remaining := initTimeout - elapsed
				if remaining > 0 {
					p.logger.Debug("Still initializing connection",
						loggerv2.String("server", serverKey),
						loggerv2.String("elapsed", elapsed.Round(time.Second).String()),
						loggerv2.String("remaining", remaining.Round(time.Second).String()))
				} else {
					p.logger.Warn("Initialization has exceeded timeout",
						loggerv2.String("server", serverKey),
						loggerv2.String("timeout", initTimeout.String()))
				}
			case <-initCtx.Done():
				return
			case <-progressDone:
				return
			}
		}
	}()

	// Initialize the connection
	p.logger.Debug("Step 2/2: Initializing MCP connection",
		loggerv2.String("server", serverKey),
		loggerv2.String("timeout", initTimeout.String()))
	initStartTime := time.Now()
	initResult, err := mcpClient.Initialize(initCtx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2024-11-05",
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "mcp-agent-go",
				Version: "1.0.0",
			},
		},
	})
	initDuration := time.Since(initStartTime)
	progressDone <- true

	if err != nil {
		_ = mcpClient.Close() // Ignore errors during cleanup
		totalDuration := time.Since(startTime)

		// Check if it was a timeout
		if initCtx.Err() == context.DeadlineExceeded {
			p.logger.Error("Initialization timed out", err,
				loggerv2.String("server", serverKey),
				loggerv2.String("init_duration", initDuration.String()),
				loggerv2.String("total_duration", totalDuration.String()))
			return nil, fmt.Errorf("failed to initialize MCP connection for %s: timed out after %v: %w",
				serverKey, initTimeout, err)
		}

		p.logger.Error("Failed to initialize MCP connection", err,
			loggerv2.String("server", serverKey),
			loggerv2.String("init_duration", initDuration.String()),
			loggerv2.String("total_duration", totalDuration.String()))
		return nil, fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	totalDuration := time.Since(startTime)
	p.logger.Debug("Connection initialized successfully",
		loggerv2.String("server", serverKey),
		loggerv2.String("init_time", initDuration.Round(time.Millisecond).String()),
		loggerv2.String("total_time", totalDuration.Round(time.Millisecond).String()))
	p.logger.Debug("Server info", loggerv2.Any("server_info", initResult.ServerInfo))

	// Get the process information if possible
	var process *os.Process
	// Note: We can't easily get the process from NewStdioMCPClient
	// This is a limitation of the mcp-go library

	conn := &StdioConnection{
		client:    mcpClient,
		process:   process,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		healthy:   true,
		serverKey: serverKey,
	}

	return conn, nil
}

// isConnectionHealthy checks if a connection is still healthy
func (p *StdioConnectionPool) isConnectionHealthy(conn *StdioConnection) bool {
	// Read lock to check health status and get client reference
	conn.mutex.RLock()
	healthy := conn.healthy
	createdAt := conn.createdAt
	client := conn.client
	serverKey := conn.serverKey
	conn.mutex.RUnlock()

	if !healthy {
		return false
	}

	// Check if connection is too old (max 1 hour)
	if time.Since(createdAt) > time.Hour {
		p.logger.Debug("Connection too old, marking unhealthy", loggerv2.String("server", serverKey))
		// Acquire write lock to update healthy status
		conn.mutex.Lock()
		conn.healthy = false
		conn.mutex.Unlock()
		return false
	}

	// Try to make a simple call to test the connection
	// Note: We call ListTools WITHOUT holding any lock to avoid blocking
	testCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to list tools as a health check
	_, err := client.ListTools(testCtx, mcp.ListToolsRequest{})
	if err != nil {
		// 🔧 ENHANCED BROKEN PIPE DETECTION IN HEALTH CHECK
		if IsBrokenPipeError(err) {
			p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Broken pipe detected in health check, marking unhealthy: %s", serverKey), loggerv2.String("server", serverKey), loggerv2.Error(err))
		} else {
			p.logger.Debug("Health check failed, marking unhealthy",
				loggerv2.String("server", serverKey),
				loggerv2.Error(err))
		}

		// Acquire write lock to update healthy status
		conn.mutex.Lock()
		conn.healthy = false
		conn.mutex.Unlock()
		return false
	}

	return true
}

// removeConnection removes a connection from the pool
// IMPORTANT: This function assumes the caller holds p.mutex
// It does NOT call Close() - caller must close the connection outside the mutex
func (p *StdioConnectionPool) removeConnection(serverKey string) *client.Client {
	if conn, exists := p.connections[serverKey]; exists {
		p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Removing connection from pool: %s", serverKey), loggerv2.String("server", serverKey))
		delete(p.connections, serverKey)
		if conn.client != nil {
			return conn.client
		}
	}
	return nil
}

// ForceRemoveBrokenConnection forcefully removes a broken connection from the pool
func (p *StdioConnectionPool) ForceRemoveBrokenConnection(serverKey string) {
	p.mutex.Lock()
	clientToClose := p.removeConnection(serverKey)
	p.mutex.Unlock()

	// Close the connection outside the mutex to avoid blocking
	if clientToClose != nil {
		p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Closing broken connection outside mutex: %s", serverKey), loggerv2.String("server", serverKey))
		_ = clientToClose.Close() // Ignore errors during cleanup
		p.logger.Info(fmt.Sprintf("✅ [STDIO POOL] Successfully force removed broken connection: %s", serverKey), loggerv2.String("server", serverKey))
	} else {
		p.logger.Debug("No connection found to force remove", loggerv2.String("server", serverKey))
	}
}

// CloseConnection closes a specific connection
func (p *StdioConnectionPool) CloseConnection(serverKey string) {
	p.mutex.Lock()
	clientToClose := p.removeConnection(serverKey)
	p.mutex.Unlock()

	// Close the connection outside the mutex to avoid blocking
	if clientToClose != nil {
		p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Closing connection outside mutex: %s", serverKey), loggerv2.String("server", serverKey))
		_ = clientToClose.Close() // Ignore errors during cleanup
	}
}

// CloseAllConnections closes all connections in the pool
func (p *StdioConnectionPool) CloseAllConnections() {
	p.mutex.Lock()
	// Collect all clients to close
	clientsToClose := make([]*client.Client, 0, len(p.connections))
	for serverKey, conn := range p.connections {
		p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Removing connection from pool: %s", serverKey), loggerv2.String("server", serverKey))
		if conn.client != nil {
			clientsToClose = append(clientsToClose, conn.client)
		}
	}
	p.connections = make(map[string]*StdioConnection)
	p.mutex.Unlock()

	// Close all connections outside the mutex to avoid blocking
	p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Closing %d connections outside mutex", len(clientsToClose)), loggerv2.Int("count", len(clientsToClose)))
	for _, client := range clientsToClose {
		_ = client.Close() // Ignore errors during cleanup
	}
}

// GetPoolStats returns statistics about the connection pool
func (p *StdioConnectionPool) GetPoolStats() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_connections": len(p.connections),
		"max_size":          p.maxSize,
		"connections":       make(map[string]interface{}),
	}

	for serverKey, conn := range p.connections {
		conn.mutex.RLock()
		stats["connections"].(map[string]interface{})[serverKey] = map[string]interface{}{
			"created_at": conn.createdAt,
			"last_used":  conn.lastUsed,
			"healthy":    conn.healthy,
			"age":        time.Since(conn.createdAt),
		}
		conn.mutex.RUnlock()
	}

	return stats
}

// startCleanupRoutine starts the background cleanup routine
func (p *StdioConnectionPool) startCleanupRoutine() {
	p.cleanupTicker = time.NewTicker(5 * time.Minute)

	go func() {
		for {
			select {
			case <-p.cleanupTicker.C:
				p.cleanupStaleConnections()
			case <-p.cleanupDone:
				p.logger.Debug("Cleanup routine stopped")
				return
			}
		}
	}()
}

// cleanupStaleConnections removes stale connections
func (p *StdioConnectionPool) cleanupStaleConnections() {
	p.mutex.Lock()

	p.logger.Debug("Running cleanup routine")

	// Collect stale connections to close
	clientsToClose := make([]*client.Client, 0)
	keysToRemove := make([]string, 0)

	for serverKey, conn := range p.connections {
		conn.mutex.RLock()
		age := time.Since(conn.createdAt)
		lastUsed := time.Since(conn.lastUsed)
		conn.mutex.RUnlock()

		// Remove connections that are too old or haven't been used recently
		if age > time.Hour || lastUsed > 30*time.Minute {
			p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Marking stale connection for removal: %s", serverKey), loggerv2.String("server", serverKey), loggerv2.String("age", age.String()), loggerv2.String("last_used", lastUsed.String()))
			keysToRemove = append(keysToRemove, serverKey)
			if conn.client != nil {
				clientsToClose = append(clientsToClose, conn.client)
			}
		}
	}

	// Remove from map while holding lock
	for _, key := range keysToRemove {
		delete(p.connections, key)
	}

	p.mutex.Unlock()

	// Close all stale connections outside the mutex to avoid blocking
	if len(clientsToClose) > 0 {
		p.logger.Info(fmt.Sprintf("🔧 [STDIO POOL] Closing %d stale connections outside mutex", len(clientsToClose)), loggerv2.Int("count", len(clientsToClose)))
		for _, client := range clientsToClose {
			_ = client.Close() // Ignore errors during cleanup
		}
	}
}

// Stop stops the connection pool and cleans up resources
func (p *StdioConnectionPool) Stop() {
	p.logger.Debug("Stopping connection pool")

	// Stop cleanup routine
	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
		p.cleanupDone <- true
	}

	// Close all connections
	p.CloseAllConnections()
}
