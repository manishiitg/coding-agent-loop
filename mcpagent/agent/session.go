// session.go
//
// This file provides public API for session-scoped connection management.
// When agents are created with SessionID, their MCP connections are stored
// in a shared session registry and persist until explicitly closed.
//
// Exported:
//   - CloseSession: Close all connections for a session
//   - CloseSessionServer: Close a specific server's connection in a session
//   - GetSessionStats: Get statistics about a session's connections
//   - GetAllSessionStats: Get statistics about all sessions
//   - GetSessionConnections: Get server names for a session
//   - ListSessions: List all active session IDs
//   - CloseAllSessions: Close all sessions (for graceful shutdown)
//   - GetSessionRegistry: Get the underlying session registry (for advanced use)

package mcpagent

import (
	"github.com/manishiitg/mcpagent/mcpclient"
)

// CloseSession closes all MCP connections for the given session ID.
// This should be called when a workflow/conversation ends to release resources.
//
// Example usage:
//
//	// At the end of a workflow
//	mcpagent.CloseSession("workflow-123")
func CloseSession(sessionID string) {
	registry := mcpclient.GetSessionRegistry()
	registry.CloseSession(sessionID)
}

// CloseSessionServer closes a specific server's connection within a session.
// Useful for reconnecting to a specific server without closing all connections.
//
// Example usage:
//
//	// Close just the playwright connection for reconnection
//	mcpagent.CloseSessionServer("workflow-123", "playwright")
func CloseSessionServer(sessionID, serverName string) {
	registry := mcpclient.GetSessionRegistry()
	registry.CloseSessionServer(sessionID, serverName)
}

// GetSessionStats returns statistics about a session's connections.
// Returns nil if the session doesn't exist.
//
// Example usage:
//
//	stats := mcpagent.GetSessionStats("workflow-123")
//	if stats != nil {
//	    fmt.Printf("Session has %d connections: %v\n", stats.ConnectionCount, stats.ServerNames)
//	}
func GetSessionStats(sessionID string) *mcpclient.SessionStats {
	registry := mcpclient.GetSessionRegistry()
	return registry.GetSessionStats(sessionID)
}

// GetAllSessionStats returns statistics about all active sessions.
// Returns a map with session IDs as keys and their stats as values.
//
// Example usage:
//
//	allStats := mcpagent.GetAllSessionStats()
//	for sessionID, stats := range allStats {
//	    fmt.Printf("Session %s has %d connections\n", sessionID, stats.ConnectionCount)
//	}
func GetAllSessionStats() map[string]*mcpclient.SessionStats {
	registry := mcpclient.GetSessionRegistry()
	sessions := registry.ListSessions()

	result := make(map[string]*mcpclient.SessionStats)
	for _, sessionID := range sessions {
		if stats := registry.GetSessionStats(sessionID); stats != nil {
			result[sessionID] = stats
		}
	}
	return result
}

// GetSessionConnections returns server names for all connections in a session.
// Useful for debugging and monitoring active connections.
//
// Example usage:
//
//	servers := mcpagent.GetSessionConnections("workflow-123")
//	fmt.Printf("Active servers: %v\n", servers)
func GetSessionConnections(sessionID string) []string {
	registry := mcpclient.GetSessionRegistry()
	connections := registry.GetSessionConnections(sessionID)
	if connections == nil {
		return nil
	}

	servers := make([]string, 0, len(connections))
	for serverName := range connections {
		servers = append(servers, serverName)
	}
	return servers
}

// ListSessions returns all active session IDs.
// Useful for debugging and monitoring.
//
// Example usage:
//
//	sessions := mcpagent.ListSessions()
//	fmt.Printf("Active sessions: %v\n", sessions)
func ListSessions() []string {
	registry := mcpclient.GetSessionRegistry()
	return registry.ListSessions()
}

// CloseAllSessions closes all sessions and their connections.
// Useful for graceful shutdown.
//
// Example usage:
//
//	// On application shutdown
//	mcpagent.CloseAllSessions()
func CloseAllSessions() {
	registry := mcpclient.GetSessionRegistry()
	registry.CloseAllSessions()
}

// HasSession checks if a session exists in the registry.
//
// Example usage:
//
//	if mcpagent.HasSession("workflow-123") {
//	    // Session has active connections
//	}
func HasSession(sessionID string) bool {
	registry := mcpclient.GetSessionRegistry()
	return registry.HasSession(sessionID)
}

// GetSessionRegistry returns the underlying session connection registry.
// This is for advanced use cases where direct registry access is needed.
func GetSessionRegistry() *mcpclient.SessionConnectionRegistry {
	return mcpclient.GetSessionRegistry()
}
