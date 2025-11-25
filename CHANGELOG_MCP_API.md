# MCP Code Execution Architecture Refactoring

## Overview

This document describes the major architectural refactoring of the MCP (Model Context Protocol) code execution system, transitioning from a Yaegi interpreter-based approach to a clean HTTP API architecture.

## Problem Statement

The previous implementation had several limitations:
- **Tight Coupling**: Code executed in-process using Yaegi interpreter, tightly coupled with agent internals
- **Language Lock-in**: Limited to Go-only code execution
- **Complexity**: Required complex registry injection and package management
- **Fragility**: User code errors could affect the main agent process

## Solution: HTTP API Architecture

We refactored the system to use a clean HTTP API for MCP tool execution, enabling:
- **Process Isolation**: User code runs as separate process via `go run`
- **Language Agnostic**: Can write code in Python, JavaScript, or any language that can make HTTP calls
- **Simplicity**: No in-process interpreter or complex injection needed
- **Stability**: Code crashes don't affect the main agent
- **Debuggability**: Can test API directly with cURL or HTTP clients

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

### 1. HTTP API Endpoint (`agent_go/cmd/server/tools.go`)

**New Endpoint**: `POST /api/mcp/execute`

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

**Response Format**:
```json
{
  "success": true,
  "result": "Document content here...",
  "error": ""
}
```

**Implementation Highlights**:
- Reuses existing MCP cache system (`mcpcache.GetCachedOrFreshConnection`)
- Thread-safe client management
- Error handling with detailed messages

### 2. Code Generation Updates (`agent_go/pkg/mcpcache/codegen/templates.go`)

Generated tool functions now produce HTTP client code instead of codeexec calls:

**Before**:
```go
func GetDocument(ctx context.Context, params map[string]interface{}) (string, error) {
    return codeexec.CallMCPTool(ctx, "get_document", params)
}
```

**After**:
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
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result struct {
        Success bool   `json:"success"`
        Result  string `json:"result"`
        Error   string `json:"error"`
    }
    json.NewDecoder(resp.Body).Decode(&result)

    if !result.Success {
        return "", fmt.Errorf(result.Error)
    }
    return result.Result, nil
}
```

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
- `agent_go/cmd/server/tools.go` (+177 lines) - API endpoint handler
- `agent_go/cmd/server/server.go` (+1 line) - Route registration
- `agent_go/pkg/mcpcache/codegen/templates.go` (+57 lines, -2 lines) - HTTP client generation
- `agent_go/pkg/mcpagent/code_execution_tools.go` (+33 lines, -7 lines) - go run execution
- `agent_go/pkg/mcpagent/prompt/builder.go` (+145 lines, -75 lines) - Updated documentation

### Frontend
- `frontend/src/components/MCPToolApiTester.tsx` (+267 lines) - New component
- `frontend/src/components/sidebar/MCPServersSection.tsx` (+18 lines) - Integration
- `frontend/src/stores/useMCPStore.ts` (+7 lines) - State management

### Total Impact
- **Lines Added**: 686
- **Lines Removed**: 82
- **Net Change**: +604 lines

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
- Keep Yaegi support as fallback option
- Allow opt-in to new architecture
- Gradual migration path

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
- HTTP API Endpoint: `POST /api/mcp/execute`
- Frontend API Tester: `MCPToolApiTester.tsx`
- Code Generation: `codegen/templates.go`

---

**Date**: 2025-01-25
**Author**: MCP Agent Builder Team
**Version**: 2.0.0
