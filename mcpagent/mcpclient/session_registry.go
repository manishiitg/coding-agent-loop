package mcpclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// SessionConnectionRegistry manages MCP connections scoped to session lifecycle.
//
// Design principles:
// - Connections are created once per (sessionID, serverName) pair
// - Multiple agents in same session share connections
// - Connections persist until CloseSession() is explicitly called
// - Per-key mutexes prevent duplicate subprocess spawns when concurrent goroutines
//   request the same server connection simultaneously
//
// Usage:
//
//	registry := GetSessionRegistry()
//	client, wasCreated, err := registry.GetOrCreateConnection(ctx, "session-123", "playwright", config, logger)
//	// ... use client ...
//	registry.CloseSession("session-123") // At workflow end
type SessionConnectionRegistry struct {
	// sessionID -> *sessionConnections
	sessions sync.Map
	// Per-key mutexes to serialize connection creation for the same (session, server) pair.
	// Prevents N goroutines from spawning N subprocesses for the same server.
	connLocks   map[string]*sync.Mutex
	connLocksMu sync.Mutex
}

// sessionConnections holds all connections for a single session
type sessionConnections struct {
	// serverName -> ClientInterface
	clients sync.Map
	// Track creation time for debugging
	createdAt time.Time
}

// Global singleton registry
var globalSessionRegistry = &SessionConnectionRegistry{
	connLocks: make(map[string]*sync.Mutex),
}

// GetSessionRegistry returns the global session connection registry
func GetSessionRegistry() *SessionConnectionRegistry {
	return globalSessionRegistry
}

// getConnLock returns a mutex for the given (session, server) key, creating one if needed.
func (r *SessionConnectionRegistry) getConnLock(key string) *sync.Mutex {
	r.connLocksMu.Lock()
	defer r.connLocksMu.Unlock()
	if mu, ok := r.connLocks[key]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	r.connLocks[key] = mu
	return mu
}

// GetOrCreateConnection returns existing connection or creates new one.
// Uses per-key mutex to ensure only one goroutine connects per (session, server) pair;
// concurrent callers block until the first connection attempt completes.
//
// Parameters:
//   - ctx: context for connection timeout
//   - sessionID: unique identifier for the session (workflow/conversation)
//   - serverName: MCP server name (e.g., "playwright", "context7")
//   - config: MCP server configuration
//   - logger: logger for connection events
//
// Returns:
//   - ClientInterface: the connection (new or existing)
//   - wasCreated: true if new connection was created, false if reused
//   - error: connection error if creation failed
func (r *SessionConnectionRegistry) GetOrCreateConnection(
	ctx context.Context,
	sessionID string,
	serverName string,
	config MCPServerConfig,
	logger loggerv2.Logger,
) (ClientInterface, bool, error) {

	// Get or create session's connection map
	sessionConnsRaw, _ := r.sessions.LoadOrStore(sessionID, &sessionConnections{
		createdAt: time.Now(),
	})
	sessionConns := sessionConnsRaw.(*sessionConnections)

	// Fast path (lock-free): connection already exists — verify it's alive
	if existing, ok := sessionConns.clients.Load(serverName); ok {
		client := existing.(ClientInterface)
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := client.Ping(pingCtx)
		pingCancel()
		if pingErr == nil {
			logger.Info(fmt.Sprintf("Reusing existing connection for session=%s server=%s", sessionID, serverName))
			return client, false, nil
		}
		// Connection is dead — close it and fall through to create a new one
		logger.Warn(fmt.Sprintf("Connection dead for session=%s server=%s, will recreate", sessionID, serverName),
			loggerv2.Error(pingErr))
		_ = client.Close()
		sessionConns.clients.Delete(serverName)
	}

	// Serialize connection creation per (session, server) key.
	// Only one goroutine will proceed to connect; others wait here and get the result.
	lockKey := sessionID + "|" + serverName
	mu := r.getConnLock(lockKey)
	mu.Lock()
	defer mu.Unlock()

	// Double-check after acquiring lock: another goroutine may have connected while we waited
	if existing, ok := sessionConns.clients.Load(serverName); ok {
		client := existing.(ClientInterface)
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := client.Ping(pingCtx)
		pingCancel()
		if pingErr == nil {
			logger.Info(fmt.Sprintf("Reusing existing connection (post-lock) for session=%s server=%s", sessionID, serverName))
			return client, false, nil
		}
		// Connection is dead — close it and fall through to create a new one
		logger.Warn(fmt.Sprintf("Connection dead (post-lock) for session=%s server=%s, will recreate", sessionID, serverName),
			loggerv2.Error(pingErr))
		_ = client.Close()
		sessionConns.clients.Delete(serverName)
	}

	// Create new connection — only one goroutine reaches here per key
	logger.Info(fmt.Sprintf("Creating new connection for session=%s server=%s", sessionID, serverName))
	client := New(config, logger)
	if err := client.ConnectWithRetry(ctx); err != nil {
		return nil, false, fmt.Errorf("failed to connect to %s: %w", serverName, err)
	}

	sessionConns.clients.Store(serverName, client)
	logger.Info(fmt.Sprintf("New connection established for session=%s server=%s", sessionID, serverName))
	return client, true, nil
}

// GetSessionConnections returns all connections for a session.
// Used when agent needs to access all its MCP clients.
func (r *SessionConnectionRegistry) GetSessionConnections(sessionID string) map[string]ClientInterface {
	sessionConnsRaw, ok := r.sessions.Load(sessionID)
	if !ok {
		return nil
	}

	sessionConns := sessionConnsRaw.(*sessionConnections)
	result := make(map[string]ClientInterface)
	sessionConns.clients.Range(func(key, value interface{}) bool {
		result[key.(string)] = value.(ClientInterface)
		return true
	})
	return result
}

// HasSession checks if a session exists in the registry
func (r *SessionConnectionRegistry) HasSession(sessionID string) bool {
	_, ok := r.sessions.Load(sessionID)
	return ok
}

// CloseSession closes all connections for a session and removes it from registry.
// Should be called when workflow/conversation ends.
//
// This is the ONLY way connections get closed when using sessions.
// Agent.Close() does NOT close connections when SessionID is set.
func (r *SessionConnectionRegistry) CloseSession(sessionID string) {
	sessionConnsRaw, ok := r.sessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}

	sessionConns := sessionConnsRaw.(*sessionConnections)
	sessionConns.clients.Range(func(key, value interface{}) bool {
		serverName := key.(string)
		if client, ok := value.(ClientInterface); ok {
			fmt.Printf("Closing connection for session=%s server=%s\n", sessionID, serverName)
			_ = client.Close()
		}
		return true
	})
}

// CloseSessionServer closes a specific server connection within a session.
// Useful for targeted cleanup (e.g., close browser but keep other connections).
func (r *SessionConnectionRegistry) CloseSessionServer(sessionID, serverName string) {
	sessionConnsRaw, ok := r.sessions.Load(sessionID)
	if !ok {
		return
	}

	sessionConns := sessionConnsRaw.(*sessionConnections)
	if clientRaw, ok := sessionConns.clients.LoadAndDelete(serverName); ok {
		if client, ok := clientRaw.(ClientInterface); ok {
			_ = client.Close()
		}
	}
}

// ListSessions returns all active session IDs (for debugging)
func (r *SessionConnectionRegistry) ListSessions() []string {
	var sessions []string
	r.sessions.Range(func(key, value interface{}) bool {
		sessions = append(sessions, key.(string))
		return true
	})
	return sessions
}

// SessionStats returns statistics about a session (for debugging)
type SessionStats struct {
	SessionID       string
	ConnectionCount int
	ServerNames     []string
	CreatedAt       time.Time
}

// GetSessionStats returns statistics about a session's connections.
// Useful for debugging and monitoring.
func (r *SessionConnectionRegistry) GetSessionStats(sessionID string) *SessionStats {
	sessionConnsRaw, ok := r.sessions.Load(sessionID)
	if !ok {
		return nil
	}

	sessionConns := sessionConnsRaw.(*sessionConnections)
	stats := &SessionStats{
		SessionID: sessionID,
		CreatedAt: sessionConns.createdAt,
	}

	sessionConns.clients.Range(func(key, value interface{}) bool {
		stats.ServerNames = append(stats.ServerNames, key.(string))
		stats.ConnectionCount++
		return true
	})

	return stats
}

// CloseAllSessions closes all sessions and their connections.
// Useful for graceful shutdown.
func (r *SessionConnectionRegistry) CloseAllSessions() {
	var sessionsToClose []string
	r.sessions.Range(func(key, value interface{}) bool {
		sessionsToClose = append(sessionsToClose, key.(string))
		return true
	})

	for _, sessionID := range sessionsToClose {
		r.CloseSession(sessionID)
	}
}
