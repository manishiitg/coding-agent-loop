# Session-Scoped MCP Connections

> **Status**: COMPLETED (January 2026)
>
> This document describes the session-scoped MCP connection registry that enables connection sharing across agents in a workflow, fixing the "Browser is already in use" error with Playwright MCP.

---

## Part 1: STDIO Connection Pool Deadlock Fix (Completed December 2025)

### Overview

This document describes a critical deadlock issue that was identified and fixed in the STDIO connection pool implementation. The fix ensures that blocking I/O operations never occur while holding mutex locks, preventing system-wide deadlocks.

### The Problem

The system would freeze when:
1. A tool execution (e.g., `browser_run_code` from playwright) was running
2. The STDIO connection pool cleanup routine triggered (every 5 minutes)
3. A hung or crashed stdio process caused `client.Close()` to block indefinitely

**Result**: All threads blocked, system completely frozen.

### Root Cause

The `removeConnection()` function was calling blocking I/O (`conn.client.Close()`) while callers held the pool mutex.

### The Fix

**Key Principle**: Never call blocking I/O operations while holding mutex locks.

Modified `removeConnection()` to return the client instead of closing it, then close outside the mutex.

**Status**: Completed December 2025

---

## Part 2: Session-Scoped Connection Registry (COMPLETED - January 2026)

### Problem Statement

When multiple agents are created sequentially (e.g., for different workflow steps), each agent creates **new MCP connections**. For Playwright MCP, this causes "Browser is already in use" errors because:

1. Agent 1 creates Playwright MCP connection → Browser opens with profile lock
2. Agent 1 closes → But browser process may still be running
3. Agent 2 creates NEW Playwright MCP connection → Tries to use same profile → **CONFLICT**

### Solution: Session-Scoped Connection Registry

Instead of creating new connections per agent, connections are scoped to a **session** (workflow/conversation):

```
Session Start (Workflow begins)
    ↓
Step 1: Agent created → Registry.GetOrCreate("session-123", "playwright") → NEW connection, browser opens
Step 1: Agent closes → Connection stays in registry, browser stays open
    ↓
Step 2: Agent created → Registry.GetOrCreate("session-123", "playwright") → REUSE existing connection
Step 2: Agent closes → Connection stays in registry
    ↓
Step 3: Agent created → REUSE same connection
    ↓
Session End (Workflow completes)
    ↓
Registry.CloseSession("session-123") → All connections closed, browser closes
```

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                 SessionConnectionRegistry                    │
│                      (sync.Map)                              │
├─────────────────────────────────────────────────────────────┤
│  "session-abc-123":                                          │
│    ├── "playwright" → ClientInterface (browser open)        │
│    ├── "context7"   → ClientInterface                       │
│    └── "filesystem" → ClientInterface                       │
│                                                              │
│  "session-xyz-456":                                          │
│    ├── "playwright" → ClientInterface (different browser)   │
│    └── "context7"   → ClientInterface                       │
└─────────────────────────────────────────────────────────────┘
```

---

## Files to Change

### 1. NEW: `mcpagent/mcpclient/session_registry.go`

```go
package mcpclient

import (
    "context"
    "fmt"
    "sync"
    "time"
)

// SessionConnectionRegistry manages MCP connections scoped to session lifecycle.
// Uses sync.Map for lock-free concurrent access.
//
// Design principles:
// - Connections are created once per (sessionID, serverName) pair
// - Multiple agents in same session share connections
// - Connections persist until CloseSession() is explicitly called
type SessionConnectionRegistry struct {
    // sessionID -> *sessionConnections
    sessions sync.Map
}

// sessionConnections holds all connections for a single session
type sessionConnections struct {
    // serverName -> ClientInterface
    clients sync.Map
    // Track creation time for debugging
    createdAt time.Time
}

// Global singleton registry
var globalSessionRegistry = &SessionConnectionRegistry{}

// GetSessionRegistry returns the global session connection registry
func GetSessionRegistry() *SessionConnectionRegistry {
    return globalSessionRegistry
}

// GetOrCreateConnection returns existing connection or creates new one.
// Thread-safe via sync.Map.LoadOrStore pattern.
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
    logger Logger,
) (ClientInterface, bool, error) {

    // Get or create session's connection map
    sessionConnsRaw, _ := r.sessions.LoadOrStore(sessionID, &sessionConnections{
        createdAt: time.Now(),
    })
    sessionConns := sessionConnsRaw.(*sessionConnections)

    // Check if connection already exists for this server
    if existing, ok := sessionConns.clients.Load(serverName); ok {
        logger.Info(fmt.Sprintf("Reusing existing connection for session=%s server=%s", sessionID, serverName))
        return existing.(ClientInterface), false, nil
    }

    // Create new connection
    logger.Info(fmt.Sprintf("Creating new connection for session=%s server=%s", sessionID, serverName))
    client := New(config, logger)
    if err := client.ConnectWithRetry(ctx); err != nil {
        return nil, false, fmt.Errorf("failed to connect to %s: %w", serverName, err)
    }

    // Store using LoadOrStore to handle race condition
    // If another goroutine created it first, use theirs
    actual, loaded := sessionConns.clients.LoadOrStore(serverName, client)
    if loaded {
        // Another goroutine created it first, close ours and use theirs
        logger.Info(fmt.Sprintf("Race condition: closing duplicate connection for session=%s server=%s", sessionID, serverName))
        _ = client.Close()
        return actual.(ClientInterface), false, nil
    }

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
// This is the ONLY way connections get closed - Agent.Close() does NOT close connections.
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
```

---

### 2. MODIFY: `mcpagent/agent/agent.go`

#### Add SessionID field to Agent struct

```go
type Agent struct {
    // ... existing fields ...

    // SessionID groups connections across multiple agents in the same workflow/conversation.
    // If set, connections are managed by SessionConnectionRegistry and persist across agent lifecycles.
    // If empty, connections are created fresh and closed when agent closes (legacy behavior).
    SessionID string

    // ... rest of fields ...
}
```

#### Modify NewAgent() to use registry when SessionID is set

```go
func NewAgent(ctx context.Context, llm llmtypes.Model, configPath string, options ...AgentOption) (*Agent, error) {
    // ... existing initialization ...

    var clients map[string]mcpclient.ClientInterface
    var err error

    // Check if session-scoped connection management is enabled
    if ag.SessionID != "" {
        // Use session registry - connections are shared and persist
        clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err =
            NewAgentConnectionWithSession(ctx, llm, serverName, configPath, ag.SessionID, string(ag.TraceID), ag.Tracers, logger, ag.DisableCache)
    } else {
        // Legacy behavior - connections are created fresh
        clients, toolToServer, allLLMTools, servers, prompts, resources, systemPrompt, err =
            NewAgentConnection(ctx, llm, serverName, configPath, string(ag.TraceID), ag.Tracers, logger, ag.DisableCache)
    }

    if err != nil {
        return nil, err
    }

    // ... rest of initialization ...
}
```

#### Modify Close() to handle both modes

```go
func (a *Agent) Close() {
    // Stop periodic cleanup routine
    a.stopCleanupRoutine()

    // Connection cleanup depends on whether session is used
    if a.SessionID == "" {
        // MODE 2 (No Session): Legacy behavior - close connections with agent
        // Each agent owns its connections, so close them here
        for serverName, client := range a.Clients {
            if client != nil {
                a.Logger.Info(fmt.Sprintf("Closing connection to %s (no session)", serverName))
                _ = client.Close()
            }
        }
    } else {
        // MODE 1 (With Session): Don't close connections
        // Connections are managed by SessionRegistry, will be closed via CloseSession()
        a.Logger.Info(fmt.Sprintf("Agent closing, connections preserved in session %s", a.SessionID))
        // Don't close - just release references
    }

    // Clear local references (both modes)
    a.Clients = nil
    a.Client = nil

    // ... rest of cleanup ...
}
```

---

### 3. MODIFY: `mcpagent/agent/options.go`

Add new option:

```go
// WithSessionID sets the session ID for connection sharing.
// When set, MCP connections are managed by SessionConnectionRegistry
// and persist across multiple agents in the same session.
//
// Usage:
//   agent, _ := NewSimpleAgent(ctx, llm, configPath,
//       WithSessionID("workflow-123"),
//   )
//
// Connections are NOT closed when agent closes - call CloseSession() at workflow end.
func WithSessionID(sessionID string) AgentOption {
    return func(a *Agent) {
        a.SessionID = sessionID
    }
}
```

---

### 4. NEW: `mcpagent/agent/connection_session.go`

Create new function for session-aware connection:

```go
package mcpagent

// NewAgentConnectionWithSession creates MCP connections using the session registry.
// Connections are reused if they already exist for the session.
//
// This function should be used when agents share connections within a workflow/conversation.
// Connections are NOT closed when the agent closes - call mcpclient.GetSessionRegistry().CloseSession()
// when the workflow ends.
func NewAgentConnectionWithSession(
    ctx context.Context,
    llm llmtypes.Model,
    serverName, configPath string,
    sessionID string,
    traceID string,
    tracers []observability.Tracer,
    logger loggerv2.Logger,
    disableCache bool,
) (map[string]mcpclient.ClientInterface, map[string]string, []llmtypes.Tool, []string, map[string][]mcp.Prompt, map[string][]mcp.Resource, string, error) {

    logger.Info("NewAgentConnectionWithSession starting",
        loggerv2.String("session_id", sessionID),
        loggerv2.String("server_name", serverName))

    // Load merged MCP configuration
    config, err := mcpclient.LoadMergedConfig(configPath, logger)
    if err != nil {
        return nil, nil, nil, nil, nil, nil, "", fmt.Errorf("failed to load config: %w", err)
    }

    // Determine which servers to connect to
    var servers []string
    if serverName == "all" || serverName == "" {
        servers = config.ListServers()
    } else {
        servers = strings.Split(serverName, ",")
        for i, s := range servers {
            servers[i] = strings.TrimSpace(s)
        }
    }

    registry := mcpclient.GetSessionRegistry()
    clients := make(map[string]mcpclient.ClientInterface)
    toolToServer := make(map[string]string)
    var allTools []llmtypes.Tool
    prompts := make(map[string][]mcp.Prompt)
    resources := make(map[string][]mcp.Resource)
    var systemPrompt string

    for _, srvName := range servers {
        serverConfig, err := config.GetServer(srvName)
        if err != nil {
            logger.Warn(fmt.Sprintf("Server %s not found in config, skipping", srvName))
            continue
        }

        // Get or create connection via registry
        client, wasCreated, err := registry.GetOrCreateConnection(ctx, sessionID, srvName, serverConfig, logger)
        if err != nil {
            logger.Error(fmt.Sprintf("Failed to get/create connection for %s", srvName), err)
            continue
        }

        clients[srvName] = client

        // Discover tools (works for both new and reused connections)
        tools, err := client.DiscoverTools(ctx)
        if err != nil {
            logger.Warn(fmt.Sprintf("Failed to discover tools for %s: %v", srvName, err))
            continue
        }

        // Convert to LLM tools and build mapping
        for _, tool := range tools {
            llmTool := convertToLLMTool(tool)
            allTools = append(allTools, llmTool)
            toolToServer[tool.Name] = srvName
        }

        // Discover prompts and resources
        if serverPrompts, err := client.DiscoverPrompts(ctx); err == nil {
            prompts[srvName] = serverPrompts
        }
        if serverResources, err := client.DiscoverResources(ctx); err == nil {
            resources[srvName] = serverResources
        }

        if wasCreated {
            logger.Info(fmt.Sprintf("New connection to %s (session=%s): %d tools discovered",
                srvName, sessionID, len(tools)))
        } else {
            logger.Info(fmt.Sprintf("Reused connection to %s (session=%s): %d tools available",
                srvName, sessionID, len(tools)))
        }
    }

    return clients, toolToServer, allTools, servers, prompts, resources, systemPrompt, nil
}
```

---

### 5. NEW: Public API for orchestrators

Add to `mcpagent/agent/session.go`:

```go
// CloseSession closes all MCP connections for a session.
// Should be called by orchestrator when workflow/conversation ends.
//
// Usage:
//   // At workflow end
//   mcpagent.CloseSession(workflowID)
func CloseSession(sessionID string) {
    mcpclient.GetSessionRegistry().CloseSession(sessionID)
}

// CloseSessionServer closes a specific MCP server connection for a session.
// Useful for targeted cleanup (e.g., close browser but keep other connections).
func CloseSessionServer(sessionID, serverName string) {
    mcpclient.GetSessionRegistry().CloseSessionServer(sessionID, serverName)
}

// GetSessionStats returns statistics about a session's connections.
// Useful for debugging and monitoring.
func GetSessionStats(sessionID string) *mcpclient.SessionStats {
    return mcpclient.GetSessionRegistry().GetSessionStats(sessionID)
}
```

---

## Usage in agent_go Orchestrator

### In StepBasedWorkflowOrchestrator

```go
// Add sessionID field
type StepBasedWorkflowOrchestrator struct {
    // ... existing fields ...
    sessionID string  // MCP connection session ID
}

// Generate session ID at workflow start
func (o *StepBasedWorkflowOrchestrator) StartWorkflow() error {
    o.sessionID = uuid.New().String()
    o.logger.Info(fmt.Sprintf("Workflow started with session ID: %s", o.sessionID))
    // ... rest of startup
}

// Pass session ID when creating agents
func (o *StepBasedWorkflowOrchestrator) createAgentForStep(step Step) (*mcpagent.Agent, error) {
    agent, err := mcpagent.NewSimpleAgent(ctx, llm, configPath,
        mcpagent.WithSessionID(o.sessionID),  // Share connections across steps
        mcpagent.WithServerName(step.ServerName),
        // ... other options
    )
    return agent, err
}

// Close session at workflow end
func (o *StepBasedWorkflowOrchestrator) EndWorkflow() {
    // Close all MCP connections for this workflow
    mcpagent.CloseSession(o.sessionID)
    o.logger.Info(fmt.Sprintf("Workflow ended, closed session: %s", o.sessionID))
}
```

---

## Sequence Diagram

```
Orchestrator                  Agent 1                  Agent 2                  Registry
     |                           |                        |                        |
     |-- StartWorkflow() ------->|                        |                        |
     |   sessionID = "abc-123"   |                        |                        |
     |                           |                        |                        |
     |-- CreateAgent(sessionID)->|                        |                        |
     |                           |-- GetOrCreate -------->|                        |
     |                           |   ("abc-123","playwright")                      |
     |                           |<-- NEW client ---------|                        |
     |                           |   (browser opens)      |                        |
     |                           |                        |                        |
     |<-- agent1 ready ----------|                        |                        |
     |                           |                        |                        |
     |-- agent1.Execute() ------>|                        |                        |
     |<-- result ----------------|                        |                        |
     |                           |                        |                        |
     |-- agent1.Close() -------->|                        |                        |
     |   (connections preserved) |                        |                        |
     |                           |                        |                        |
     |-- CreateAgent(sessionID)------------------------>|                        |
     |                           |                        |-- GetOrCreate ------->|
     |                           |                        |   ("abc-123","playwright")
     |                           |                        |<-- EXISTING client ---|
     |                           |                        |   (same browser!)     |
     |                           |                        |                        |
     |<-- agent2 ready ----------------------------------|                        |
     |                           |                        |                        |
     |-- agent2.Execute() ----------------------------->|                        |
     |<-- result ---------------------------------------|                        |
     |                           |                        |                        |
     |-- agent2.Close() -------------------------------->|                        |
     |   (connections preserved) |                        |                        |
     |                           |                        |                        |
     |-- EndWorkflow() --------- CloseSession("abc-123") ----------------------->|
     |                           |                        |   (browser closes)    |
     |                           |                        |                        |
```

---

## Testing Checklist

### Mode 1: With SessionID
- [ ] Single agent with session ID - connections created
- [ ] Second agent with SAME session ID - connections reused (no new browser)
- [ ] Agent.Close() with session ID - connections NOT closed
- [ ] CloseSession() - all connections closed
- [ ] Multiple concurrent sessions - isolated from each other
- [ ] Race condition test - multiple agents created simultaneously
- [ ] Playwright browser lock test - no "already in use" error

### Mode 2: Without SessionID (Legacy)
- [ ] Agent without session ID - connections created normally
- [ ] Agent.Close() without session ID - connections ARE closed
- [ ] Second agent without session ID - creates NEW connections (not shared)
- [ ] Backward compatibility - existing code works unchanged

---

## Two Modes of Operation

### Mode 1: With SessionID (Recommended for Orchestrators)

```go
// Create agent with session - connections are shared and persist
agent, _ := mcpagent.NewSimpleAgent(ctx, llm, configPath,
    mcpagent.WithSessionID("workflow-123"),
)

// Agent.Close() does NOT close connections
agent.Close()

// Explicitly close at workflow end
mcpagent.CloseSession("workflow-123")
```

**Behavior:**
- Connections stored in SessionRegistry
- Multiple agents with same sessionID share connections
- `Agent.Close()` only cleans up agent state, not connections
- Must call `CloseSession()` to close connections

### Mode 2: Without SessionID (Legacy / Standalone)

```go
// Create agent without session - each agent owns its connections
agent, _ := mcpagent.NewSimpleAgent(ctx, llm, configPath)

// Agent.Close() DOES close connections
agent.Close()
```

**Behavior:**
- Each agent creates its own connections
- `Agent.Close()` closes all connections
- No session registry involved
- Use for: standalone scripts, single-agent scenarios, testing

---

## Migration Notes

1. **Backward Compatible**: If `SessionID` is not set, legacy behavior is preserved (connections close with agent)
2. **Opt-in**: Use `WithSessionID()` to enable session-scoped connections
3. **Explicit Cleanup**: When using sessions, `CloseSession()` must be called - connections don't auto-cleanup

---

## Why sync.Map instead of sync.RWMutex?

| Feature | `sync.RWMutex` + map | `sync.Map` |
|---------|---------------------|------------|
| Get-or-create pattern | Manual lock/check/create | Built-in `LoadOrStore` |
| Type safety | Generic maps | `interface{}` everywhere |
| Performance (read-heavy) | Good with `RLock` | Optimized for this |
| Race condition handling | Manual | Built-in with `LoadOrStore` |

`sync.Map` is ideal because:
1. **`LoadOrStore`** handles "get existing or create new" atomically
2. **Write-once, read-many** pattern (connect once, call tools many times)
3. **Disjoint keys** - different sessions don't share keys

---

## Quick Reference (Implementation Complete)

### Files Created/Modified

| File | Description |
|------|-------------|
| `mcpclient/session_registry.go` | **NEW** - SessionConnectionRegistry with sync.Map |
| `agent/connection_session.go` | **NEW** - NewAgentConnectionWithSession function |
| `agent/session.go` | **NEW** - Public API (CloseSession, etc.) |
| `agent/agent.go` | **MODIFIED** - Added SessionID field, WithSessionID option, updated NewAgent() and Close() |

### Public API

```go
import mcpagent "github.com/manishiitg/mcpagent/agent"

// Create agent with session (connections shared across agents in same session)
agent, err := mcpagent.NewAgent(ctx, llm, configPath,
    mcpagent.WithSessionID("workflow-123"))

// Later, close all connections for the session
mcpagent.CloseSession("workflow-123")

// Other session management functions:
mcpagent.ListSessions()                           // List all active session IDs
mcpagent.GetSessionStats("workflow-123")          // Get stats for a session
mcpagent.GetSessionConnections("workflow-123")    // Get server names for a session
mcpagent.CloseSessionServer("workflow-123", "playwright") // Close specific server
mcpagent.CloseAllSessions()                       // Close all (for graceful shutdown)
mcpagent.HasSession("workflow-123")               // Check if session exists
```

### Key Behaviors

1. **With SessionID**: Connections persist in registry, Agent.Close() does NOT close them
2. **Without SessionID**: Legacy behavior - Agent.Close() closes all connections
3. **Session Cleanup**: Must call `CloseSession()` when workflow ends to release resources
