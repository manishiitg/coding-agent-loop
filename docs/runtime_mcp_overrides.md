# Runtime MCP Configuration Overrides

## Overview

Runtime MCP overrides allow workflow-specific modifications to MCP server configurations at execution time. This is useful for:

- Setting unique output directories per workflow run
- Adding workflow-specific environment variables
- Appending additional command arguments dynamically

## Problem Solved

When running multiple workflows concurrently, they may need isolated resources. For example, Playwright MCP server uses a shared browser profile by default, causing "Browser is already in use" errors when two workflows try to use it simultaneously.

**Error example:**
```
Error: Browser is already in use for /Users/mipl/Library/Caches/ms-playwright/mcp-chrome
```

## Solution Architecture

```
Workflow (has run folder context)
    |
    v
AgentConfig.RuntimeOverrides
    |
    v
NewBaseAgent() -> mcpagent.WithRuntimeOverrides()
    |
    v
NewAgentConnection() / NewAgentConnectionWithSession()
    |
    v
GetCachedOrFreshConnection()
    |
    v
performOriginalConnectionLogic() -> MCPServerConfig.ApplyOverride()
    |
    v
MCP Server spawned with modified args/env
```

## Data Structures

### RuntimeConfigOverride

```go
// mcpagent/mcpclient/config.go

type RuntimeConfigOverride struct {
    // ArgsReplace replaces specific arg values by flag name
    // e.g., {"--output-dir": "/new/path"} finds "--output-dir" and replaces next arg
    ArgsReplace map[string]string `json:"args_replace,omitempty"`

    // ArgsAppend appends additional args to the command
    ArgsAppend []string `json:"args_append,omitempty"`

    // EnvOverride adds or overrides environment variables
    EnvOverride map[string]string `json:"env_override,omitempty"`
}

// RuntimeOverrides maps server names to their runtime configuration overrides
type RuntimeOverrides map[string]RuntimeConfigOverride
```

### AgentConfig Field

```go
// agent_go/pkg/orchestrator/agents/interfaces.go

type AgentConfig struct {
    // ... other fields ...

    // Runtime config overrides for MCP servers
    // Allows workflow-specific modifications like output directories per run
    RuntimeOverrides mcpclient.RuntimeOverrides `json:"runtime_overrides,omitempty"`
}
```

## Usage Examples

### Example 1: Set Playwright Output Directory Per Workflow

```go
import "mcpagent/mcpclient"

// In workflow orchestrator or step executor
runFolder := "/path/to/runs/iteration-3/user123/execution"

runtimeOverrides := mcpclient.RuntimeOverrides{
    "playwright": {
        ArgsReplace: map[string]string{
            "--output-dir": filepath.Join(runFolder, "downloads"),
        },
    },
}

// Pass to agent config
agentConfig.RuntimeOverrides = runtimeOverrides
```

### Example 2: Add Environment Variables

```go
runtimeOverrides := mcpclient.RuntimeOverrides{
    "my-server": {
        EnvOverride: map[string]string{
            "WORKFLOW_ID":   "wf-12345",
            "RUN_FOLDER":    runFolder,
            "DEBUG_ENABLED": "true",
        },
    },
}
```

### Example 3: Append Additional Args

```go
runtimeOverrides := mcpclient.RuntimeOverrides{
    "playwright": {
        ArgsAppend: []string{
            "--headless",
            "--timeout=60000",
        },
    },
}
```

### Example 4: Combined Overrides

```go
runtimeOverrides := mcpclient.RuntimeOverrides{
    "playwright": {
        ArgsReplace: map[string]string{
            "--output-dir": "/custom/downloads",
        },
        ArgsAppend: []string{
            "--isolated",
        },
        EnvOverride: map[string]string{
            "PLAYWRIGHT_BROWSERS_PATH": "/custom/browsers",
        },
    },
}
```

## How ArgsReplace Works

The `ArgsReplace` map finds flags and replaces their values. It handles two formats:

### Format 1: Separate flag and value
```
Original: ["--output-dir", "/old/path", "--other-flag"]
ArgsReplace: {"--output-dir": "/new/path"}
Result:   ["--output-dir", "/new/path", "--other-flag"]
```

### Format 2: Combined flag=value
```
Original: ["--output-dir=/old/path", "--other-flag"]
ArgsReplace: {"--output-dir": "/new/path"}
Result:   ["--output-dir=/new/path", "--other-flag"]
```

## Integration Points

### 1. Agent Creation (mcpagent)

```go
// Using the WithRuntimeOverrides option
agent, err := mcpagent.NewAgent(
    ctx,
    llm,
    configPath,
    mcpagent.WithRuntimeOverrides(runtimeOverrides),
)
```

### 2. BaseAgent Creation (orchestrator)

```go
// Via AgentConfig
config := &agents.AgentConfig{
    // ... other config ...
    RuntimeOverrides: runtimeOverrides,
}

// NewBaseAgent automatically passes to mcpagent.WithRuntimeOverrides
```

### 3. Session-Scoped Connections

Runtime overrides are also applied when using session-scoped connection management (`MCPSessionID`). The overrides are applied before connections are created/reused.

## Files Modified

| File | Changes |
|------|---------|
| `mcpagent/mcpclient/config.go` | Added `RuntimeConfigOverride`, `RuntimeOverrides`, and `ApplyOverride()` |
| `mcpagent/mcpcache/integration.go` | Updated connection functions to accept and apply overrides |
| `mcpagent/agent/connection.go` | Pass overrides to cache layer |
| `mcpagent/agent/connection_session.go` | Apply overrides for session connections |
| `mcpagent/agent/agent.go` | Added `RuntimeOverrides` field and `WithRuntimeOverrides` option |
| `agent_go/pkg/orchestrator/agents/interfaces.go` | Added `RuntimeOverrides` to `AgentConfig` |
| `agent_go/pkg/orchestrator/agents/base_agent.go` | Pass overrides to mcpagent |

## Quick Fix: Playwright Isolation

For immediate Playwright isolation without dynamic overrides, add `--isolated` to the config:

```json
// agent_go/configs/mcp_servers_clean.json
{
  "mcpServers": {
    "playwright": {
      "command": "npx",
      "args": [
        "@playwright/mcp@latest",
        "--output-dir",
        "/path/to/downloads",
        "--isolated"
      ]
    }
  }
}
```

The `--isolated` flag makes each Playwright instance use a temporary browser profile instead of sharing one.

## Best Practices

1. **Use run folder paths**: Set output directories relative to the workflow run folder for proper isolation
2. **Clean up**: Files in run folders can be cleaned up when workflows complete
3. **Logging**: Runtime overrides are logged when applied for debugging
4. **Nil safety**: Pass `nil` when no overrides needed - the system handles this gracefully

## Related Documentation

- [Running Workflows](running_workflows.md) - Background workflow execution
- [Session-Scoped MCP Connections](../../mcpagent/docs/session_scoped_mcp_connections.md) - Connection sharing
