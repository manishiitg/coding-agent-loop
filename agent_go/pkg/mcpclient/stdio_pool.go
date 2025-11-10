package mcpclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"mcp-agent/agent_go/internal/utils"

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
	logger        utils.ExtendedLogger
	cleanupTicker *time.Ticker
	cleanupDone   chan bool
}

// NewStdioConnectionPool creates a new stdio connection pool
func NewStdioConnectionPool(maxSize int, logger utils.ExtendedLogger) *StdioConnectionPool {
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
	p.logger.Infof("🔧 [STDIO POOL] Getting connection for server: %s", serverKey)

	// Check if we have an existing connection
	var existingConn *StdioConnection
	var needsHealthCheck bool
	if conn, exists := p.connections[serverKey]; exists {
		existingConn = conn
		// Quick check if already marked unhealthy
		conn.mutex.RLock()
		needsHealthCheck = conn.healthy
		conn.mutex.RUnlock()

		if !needsHealthCheck {
			// Connection already marked unhealthy, remove it
			p.logger.Infof("❌ [STDIO POOL] Existing connection unhealthy, removing: %s", serverKey)
			p.removeConnection(serverKey)
			existingConn = nil
		}
	}
	p.mutex.Unlock() // Unlock before any potentially long-running operations

	// If we have an existing connection, check its health (this can take time)
	if existingConn != nil && needsHealthCheck {
		if p.isConnectionHealthy(existingConn) {
			// Re-acquire lock to update lastUsed
			p.mutex.Lock()
			// Double-check connection still exists and is the same one
			if conn, stillExists := p.connections[serverKey]; stillExists && conn == existingConn {
				p.logger.Infof("✅ [STDIO POOL] Reusing existing healthy connection for server: %s", serverKey)
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
			if _, stillExists := p.connections[serverKey]; stillExists {
				p.logger.Infof("❌ [STDIO POOL] Existing connection unhealthy, removing: %s", serverKey)
				p.removeConnection(serverKey)
			}
			p.mutex.Unlock()
		}
	}

	// Create new connection WITHOUT holding the pool mutex
	// This prevents blocking other goroutines during the potentially long initialization (up to 10 minutes)
	p.logger.Infof("🔧 [STDIO POOL] Creating new connection for server: %s", serverKey)
	conn, err := p.createNewConnection(ctx, serverKey, command, args, env)
	if err != nil {
		return nil, fmt.Errorf("failed to create new stdio connection: %w", err)
	}

	// Re-acquire lock to add connection to pool
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Double-check another goroutine didn't create the connection while we were creating ours
	if existingConn, exists := p.connections[serverKey]; exists {
		p.logger.Infof("🔧 [STDIO POOL] Another goroutine created connection for %s, using existing one", serverKey)
		// Close our newly created connection and use the existing one
		if conn.client != nil {
			conn.client.Close()
		}
		return existingConn.client, nil
	}

	p.connections[serverKey] = conn
	p.logger.Infof("✅ [STDIO POOL] New connection created and added to pool: %s", serverKey)

	return conn.client, nil
}

// createNewConnection creates a new stdio connection
func (p *StdioConnectionPool) createNewConnection(ctx context.Context, serverKey string, command string, args []string, env []string) (*StdioConnection, error) {
	startTime := time.Now()
	p.logger.Infof("🔧 [STDIO POOL] Creating new stdio connection: %s %v", command, args)

	// Create the MCP client
	p.logger.Infof("🔧 [STDIO POOL] Step 1/2: Creating stdio MCP client for: %s", serverKey)
	clientStartTime := time.Now()
	mcpClient, err := client.NewStdioMCPClient(command, env, args...)
	if err != nil {
		clientDuration := time.Since(clientStartTime)
		p.logger.Errorf("❌ [STDIO POOL] Failed to create stdio client after %v: %w", clientDuration, err)
		return nil, fmt.Errorf("failed to create stdio client: %w", err)
	}
	clientDuration := time.Since(clientStartTime)
	p.logger.Infof("✅ [STDIO POOL] Stdio MCP client created successfully in %v", clientDuration.Round(time.Millisecond))

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
					p.logger.Infof("⏳ [STDIO POOL] Still initializing connection for %s... (elapsed: %v, remaining: %v)",
						serverKey, elapsed.Round(time.Second), remaining.Round(time.Second))
				} else {
					p.logger.Warnf("⚠️ [STDIO POOL] Initialization for %s has exceeded timeout (%v)", serverKey, initTimeout)
				}
			case <-initCtx.Done():
				return
			case <-progressDone:
				return
			}
		}
	}()

	// Initialize the connection
	p.logger.Infof("🔧 [STDIO POOL] Step 2/2: Initializing MCP connection for: %s (timeout: %v)", serverKey, initTimeout)
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
		mcpClient.Close()
		totalDuration := time.Since(startTime)

		// Check if it was a timeout
		if initCtx.Err() == context.DeadlineExceeded {
			p.logger.Errorf("❌ [STDIO POOL] Initialization timed out for %s after %v (total: %v): %v",
				serverKey, initDuration, totalDuration, err)
			return nil, fmt.Errorf("failed to initialize MCP connection for %s: timed out after %v: %w",
				serverKey, initTimeout, err)
		}

		p.logger.Errorf("❌ [STDIO POOL] Failed to initialize MCP connection for %s after %v (total: %v): %w",
			serverKey, initDuration, totalDuration, err)
		return nil, fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	totalDuration := time.Since(startTime)
	p.logger.Infof("✅ [STDIO POOL] Connection initialized successfully: %s (init_time: %v, total_time: %v)",
		serverKey, initDuration.Round(time.Millisecond), totalDuration.Round(time.Millisecond))
	p.logger.Infof("🔧 [STDIO POOL] Server info: %+v", initResult.ServerInfo)

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
		p.logger.Infof("🔧 [STDIO POOL] Connection too old, marking unhealthy: %s", serverKey)
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
		errorMessage := err.Error()
		isBrokenPipe := strings.Contains(errorMessage, "Broken pipe") ||
			strings.Contains(errorMessage, "broken pipe") ||
			strings.Contains(errorMessage, "[Errno 32]") ||
			strings.Contains(errorMessage, "EOF") ||
			strings.Contains(errorMessage, "connection reset")

		if isBrokenPipe {
			p.logger.Infof("🔧 [STDIO POOL] Broken pipe detected in health check, marking unhealthy: %s, error: %v", serverKey, err)
		} else {
			p.logger.Infof("❌ [STDIO POOL] Health check failed, marking unhealthy: %s, error: %v", serverKey, err)
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
func (p *StdioConnectionPool) removeConnection(serverKey string) {
	if conn, exists := p.connections[serverKey]; exists {
		p.logger.Infof("🔧 [STDIO POOL] Removing connection: %s", serverKey)
		if conn.client != nil {
			conn.client.Close()
		}
		delete(p.connections, serverKey)
	}
}

// ForceRemoveBrokenConnection forcefully removes a broken connection from the pool
func (p *StdioConnectionPool) ForceRemoveBrokenConnection(serverKey string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if conn, exists := p.connections[serverKey]; exists {
		p.logger.Infof("🔧 [STDIO POOL] Force removing broken connection: %s", serverKey)
		if conn.client != nil {
			conn.client.Close()
		}
		delete(p.connections, serverKey)
		p.logger.Infof("✅ [STDIO POOL] Successfully force removed broken connection: %s", serverKey)
	} else {
		p.logger.Infof("🔧 [STDIO POOL] No connection found to force remove: %s", serverKey)
	}
}

// CloseConnection closes a specific connection
func (p *StdioConnectionPool) CloseConnection(serverKey string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Infof("🔧 [STDIO POOL] Closing connection: %s", serverKey)
	p.removeConnection(serverKey)
}

// CloseAllConnections closes all connections in the pool
func (p *StdioConnectionPool) CloseAllConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Infof("🔧 [STDIO POOL] Closing all connections")
	for serverKey, conn := range p.connections {
		p.logger.Infof("🔧 [STDIO POOL] Closing connection: %s", serverKey)
		if conn.client != nil {
			conn.client.Close()
		}
	}
	p.connections = make(map[string]*StdioConnection)
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
				p.logger.Infof("🔧 [STDIO POOL] Cleanup routine stopped")
				return
			}
		}
	}()
}

// cleanupStaleConnections removes stale connections
func (p *StdioConnectionPool) cleanupStaleConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.logger.Infof("🔧 [STDIO POOL] Running cleanup routine")

	for serverKey, conn := range p.connections {
		conn.mutex.RLock()
		age := time.Since(conn.createdAt)
		lastUsed := time.Since(conn.lastUsed)
		conn.mutex.RUnlock()

		// Remove connections that are too old or haven't been used recently
		if age > time.Hour || lastUsed > 30*time.Minute {
			p.logger.Infof("🔧 [STDIO POOL] Removing stale connection: %s (age: %v, last_used: %v)", serverKey, age, lastUsed)
			p.removeConnection(serverKey)
		}
	}
}

// Stop stops the connection pool and cleans up resources
func (p *StdioConnectionPool) Stop() {
	p.logger.Infof("🔧 [STDIO POOL] Stopping connection pool")

	// Stop cleanup routine
	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
		p.cleanupDone <- true
	}

	// Close all connections
	p.CloseAllConnections()
}
