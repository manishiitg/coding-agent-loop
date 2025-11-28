# MCP Code Execution Architecture Refactoring

## Overview

This document describes the major architectural refactoring of the MCP (Model Context Protocol) code execution system, transitioning from a Yaegi interpreter-based approach to a clean HTTP API architecture.

**Status**: ✅ **COMPLETE** (Migration completed 2025-11-25)

## Problem Statement

The previous implementation had several limitations:
- **Tight Coupling**: Code executed in-process using Yaegi interpreter, tightly coupled with agent internals
- **Language Lock-in**: Limited to Go-only code execution
- **Complexity**: Required complex registry injection and package management
- **Fragility**: User code errors could affect the main agent process

## Solution: HTTP API Architecture

We refactored the system to use a clean HTTP API for **all tool types** (MCP, Custom, and Virtual), enabling:
- **Process Isolation**: User code runs as separate process via `go run`
- **Language Agnostic**: Can write code in Python, JavaScript, or any language that can make HTTP calls
- **Simplicity**: No in-process interpreter or complex injection needed
- **Stability**: Code crashes don't affect the main agent
- **Debuggability**: Can test API directly with cURL or HTTP clients
- **Yaegi Removed**: The Yaegi interpreter dependency has been completely removed

## Architecture Changes

### Before (Yaegi-based)
```
┌─────────────────────────────────┐
│      Agent Process              │
│  ┌──────────────────────────┐  │
│  │   Yaegi Interpreter       │  │
│  │  ┌────────────────────┐  │  │
│  │  │   User Go Code     │  │  │
│  │  │   (in-process)     │  │  │
│  │  └────────────────────┘  │  │
│  │          ↓                │  │
│  │   Registry Injection      │  │
│  │   (codeexec.CallMCPTool) │  │
│  └──────────────────────────┘  │
│            ↓                    │
│      MCP Client                 │
└─────────────────────────────────┘
```

### After (HTTP API-based)
```
┌─────────────────────────┐      HTTP POST          ┌─────────────────────────┐
│  Separate Process       │  /api/mcp/execute       │   Agent Process         │
│  ┌───────────────────┐  │  ───────────────────→   │  ┌──────────────────┐  │
│  │  User Go Code     │  │                         │  │  API Handler     │  │
│  │  (via go run)     │  │  ←───────────────────   │  │  (tools.go)      │  │
│  └───────────────────┘  │    JSON Response        │  └──────────────────┘  │
│         ↓                │                         │          ↓             │
│  Generated Functions    │                         │    MCP Cache           │
│  (HTTP POST calls)      │                         │          ↓             │
└─────────────────────────┘                         │    MCP Client          │
                                                     └─────────────────────────┘
```

## Implementation Details

### 1. HTTP API Endpoints (`agent_go/cmd/server/tools.go`)

Three endpoints handle different tool types:

#### MCP Tools: `POST /api/mcp/execute`
For executing MCP server tools (e.g., AWS, Google Sheets, etc.)

**Request Format**:
```json
{
  "server": "aws",
  "tool": "get_document",
  "args": {
    "document_id": "doc123"
  }
}
```

#### Custom Tools: `POST /api/custom/execute`
For executing custom tools (e.g., workspace tools, human tools)

**Request Format**:
```json
{
  "tool": "read_workspace_file",
  "args": {
    "path": "/path/to/file"
  }
}
```

#### Virtual Tools: `POST /api/virtual/execute`
For executing virtual tools (e.g., discover_code_files, write_code)

**Note**: The `discover_code_structure` tool has been retired in favor of `discover_code_files`, which provides more targeted code discovery by requiring both `server_name` and `tool_name` parameters.

**Request Format**:
```json
{
  "tool": "discover_code_files",
  "args": {
    "server_name": "aws",
    "tool_name": "GetDocument"
  }
}
```

**Response Format** (all endpoints):
```json
{
  "success": true,
  "result": "Tool output here...",
  "error": ""
}
```

**Implementation Highlights**:
- Reuses existing MCP cache system (`mcpcache.GetCachedOrFreshConnection`)
- Uses codeexec registry for custom/virtual tool execution
- Thread-safe client management
- Error handling with detailed messages
- CORS support for frontend testing

### 2. Code Generation Updates (`agent_go/pkg/mcpcache/codegen/templates.go`)

All generated tool functions now produce HTTP client code. Three template functions handle different tool types:

#### MCP Server Tools (`GenerateFunctionWithParams`)
```go
func GetDocument(params map[string]interface{}) (string, error) {
    apiURL := os.Getenv("MCP_API_URL")
    if apiURL == "" {
        apiURL = "http://localhost:8000"
    }
    reqBody, _ := json.Marshal(map[string]interface{}{
        "server": os.Getenv("MCP_SERVER_NAME"),
        "tool":   "get_document",
        "args":   params,
    })
    resp, err := http.Post(apiURL+"/api/mcp/execute", "application/json", bytes.NewBuffer(reqBody))
    // ... response handling
}
```

#### Custom Tools (`GenerateCustomToolFunction`)
```go
func ReadWorkspaceFile(params map[string]interface{}) (string, error) {
    apiURL := os.Getenv("MCP_API_URL")
    if apiURL == "" {
        apiURL = "http://localhost:8000"
    }
    reqBody, _ := json.Marshal(map[string]interface{}{
        "tool": "read_workspace_file",
        "args": params,
    })
    resp, err := http.Post(apiURL+"/api/custom/execute", "application/json", bytes.NewBuffer(reqBody))
    // ... response handling
}
```

#### Virtual Tools (`GenerateVirtualToolFunction`)
```go
func DiscoverCodeFiles(params map[string]interface{}) (string, error) {
    apiURL := os.Getenv("MCP_API_URL")
    if apiURL == "" {
        apiURL = "http://localhost:8000"
    }
    reqBody, _ := json.Marshal(map[string]interface{}{
        "tool": "discover_code_files",
        "args": params,
    })
    resp, err := http.Post(apiURL+"/api/virtual/execute", "application/json", bytes.NewBuffer(reqBody))
    // ... response handling
}
```

**Note**: The `discover_code_files` tool requires both `server_name` and `tool_name` parameters. The old `discover_code_structure` tool (which returned all available tools) has been removed, as tool structure information is now automatically included in the system prompt.

**Key Changes**:
- Removed `context.Context` parameter (not needed for HTTP calls)
- Removed `codeexec` import dependency
- Each tool type routes to its appropriate endpoint

### 3. Code Execution Handler (`agent_go/pkg/mcpagent/code_execution_tools.go`)

**Before**: Used Yaegi interpreter (`executeGoCodeViaYaegi`)
**After**: Uses `go run` command (`executeGoCode`)

```go
func (a *Agent) executeGoCode(ctx context.Context, workspaceDir, filePath, code string) (string, error) {
    cmd := exec.CommandContext(ctx, "go", "run", filePath)
    cmd.Dir = workspaceDir

    cmd.Env = append(os.Environ(),
        "MCP_API_URL=http://localhost:8000",
    )

    output, err := cmd.CombinedOutput()
    return string(output), err
}
```

### 4. Frontend API Tester (`frontend/src/components/MCPToolApiTester.tsx`)

Added interactive UI component for testing MCP tools:
- Parameter input with JSON editor
- Example argument generation
- Execute tool with loading state
- Response display with success/error formatting
- Copy as cURL command for external testing

**Features**:
- Tool description and parameter documentation
- Required vs optional parameter highlighting
- Success/error visual feedback
- Direct integration into MCP Servers section

### 5. System Prompt Updates (`agent_go/pkg/mcpagent/prompt/builder.go`)

Updated code execution mode documentation to explain:
- HTTP API architecture
- Generated function patterns
- Environment variables (MCP_API_URL, MCP_SERVER_NAME)
- Example code with multiple servers
- Direct HTTP API usage patterns

## Migration Path

### For Existing Code

Old code using codeexec will need updates:

**Old Pattern**:
```go
import "mcp-agent/agent_go/pkg/mcpagent/codeexec"

func execute() string {
    ctx := context.Background()
    params := map[string]interface{}{"document_id": "doc123"}
    output, err := codeexec.CallMCPTool(ctx, "get_document", params)
    return output
}
```

**New Pattern**:
```go
package main

import (
    "fmt"
    "os"
)

func main() {
    os.Setenv("MCP_SERVER_NAME", "aws")
    params := map[string]interface{}{"document_id": "doc123"}
    output, err := GetDocument(params)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    fmt.Println(output)
}
```

### Environment Variables

- **MCP_API_URL**: API endpoint (default: `http://localhost:8000`)
- **MCP_SERVER_NAME**: Server name for tool execution (set per-call)

## Benefits

### 1. **Language Agnostic**
Can now write code in any language:
```python
import requests
import json

response = requests.post('http://localhost:8000/api/mcp/execute', json={
    'server': 'aws',
    'tool': 'get_document',
    'args': {'document_id': 'doc123'}
})
result = response.json()
print(result['result'])
```

### 2. **Process Isolation**
User code crashes don't affect agent:
- Separate process via `go run`
- Timeout handling
- Resource limits possible

### 3. **Better Debugging**
Direct API testing:
```bash
curl -X POST http://localhost:8000/api/mcp/execute \
  -H "Content-Type: application/json" \
  -d '{
    "server": "aws",
    "tool": "get_document",
    "args": {"document_id": "doc123"}
  }'
```

### 4. **Simpler Architecture**
- No in-process interpreter needed
- No complex registry injection
- Standard HTTP patterns
- Reuses existing MCP cache

### 5. **UI Testing**
Frontend provides interactive testing:
- Browse available tools
- Test with example parameters
- Copy as cURL for sharing
- See responses in real-time

## Files Changed

### Backend
- `agent_go/cmd/server/tools.go` - Added `handleCustomExecute()` and `handleVirtualExecute()` handlers
- `agent_go/cmd/server/server.go` - Registered `/api/custom/execute` and `/api/virtual/execute` routes
- `agent_go/pkg/mcpcache/codegen/templates.go` - Updated all template functions to generate HTTP API calls
- `agent_go/pkg/mcpagent/code_execution_tools.go` - Removed Yaegi interpreter code (~400 lines removed)
- `agent_go/go.mod` - Removed `github.com/traefik/yaegi` dependency

### Removed Code (Yaegi-related)
- `executeGoCodeViaYaegi()` - In-process Go interpreter execution
- `wrapCodeForYaegi()` - Code wrapping for interpreter
- `injectGeneratedToolPackages()` - Package injection for interpreter
- `addCodeexecImport()` - Import manipulation
- `removeGeneratedToolImports()` - Import cleanup

### Frontend
- `frontend/src/components/MCPToolApiTester.tsx` - Interactive API testing component
- `frontend/src/components/sidebar/MCPServersSection.tsx` - Integration
- `frontend/src/stores/useMCPStore.ts` - State management

## Testing

### Manual Testing

1. **Start the agent**:
   ```bash
   cd agent_go
   go run cmd/server/main.go
   ```

2. **Test API directly**:
   ```bash
   curl -X POST http://localhost:8000/api/mcp/execute \
     -H "Content-Type: application/json" \
     -d '{"server":"aws","tool":"list_buckets","args":{}}'
   ```

3. **Test via UI**:
   - Open frontend at http://localhost:3000
   - Navigate to MCP Servers section
   - Click "Show" to expand server tools
   - Click "Test API" on any tool
   - Fill in parameters and execute

4. **Test generated code**:
   - Use `write_code` tool in code execution mode
   - Generated functions automatically use HTTP API
   - Code runs as separate process

### Automated Testing

Consider adding:
- Unit tests for API endpoint
- Integration tests for code execution
- Load tests for concurrent requests
- Error handling tests

## Performance Considerations

### HTTP Overhead
- Each tool call requires HTTP round-trip
- Minimal overhead (~1-2ms locally)
- Can batch multiple calls in user code

### Process Startup
- `go run` has compilation overhead
- Consider pre-compilation for production
- Cache compiled binaries

### Concurrent Requests
- API handler is thread-safe
- MCP cache handles concurrent access
- Consider rate limiting for production

## Security Considerations

### Sandboxing
- User code runs in separate process
- Can add resource limits (CPU, memory, time)
- File system access controlled by workspace

### Input Validation
- API validates server names
- Tool names checked against available tools
- Args passed through safely

### Network Isolation
- API only accessible on localhost by default
- Can add authentication for remote access
- Consider HTTPS for production

## Future Enhancements

### Potential Improvements
1. **Language Support**: Official SDKs for Python, JavaScript, etc.
2. **Streaming**: WebSocket support for long-running tools
3. **Caching**: Response caching for idempotent tools
4. **Metrics**: Request timing and success rates
5. **Authentication**: API keys or JWT tokens
6. **Rate Limiting**: Prevent abuse
7. **Batch API**: Execute multiple tools in one request

### Backward Compatibility
- **Yaegi has been completely removed** - no fallback to interpreter-based execution
- The codeexec registry remains for server-side tool execution (used by API handlers)
- Generated code uses HTTP API exclusively

## Conclusion

This refactoring significantly improves the MCP code execution architecture by:
- Decoupling user code from agent process
- Enabling multi-language support
- Simplifying implementation
- Improving stability and debuggability
- Adding interactive UI testing

The HTTP API approach provides a solid foundation for future enhancements while maintaining the flexibility and power of the MCP system.

## References

- MCP Specification: https://modelcontextprotocol.io
- HTTP API Endpoints:
  - `POST /api/mcp/execute` - MCP server tools
  - `POST /api/custom/execute` - Custom tools
  - `POST /api/virtual/execute` - Virtual tools
- Frontend API Tester: `MCPToolApiTester.tsx`
- Code Generation: `codegen/templates.go`

---

**Date**: 2025-11-25
**Author**: MCP Agent Builder Team
**Version**: 2.0.0 (Migration Complete)
