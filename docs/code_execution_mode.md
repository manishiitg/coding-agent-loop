# Code Execution Mode

## 2. Code Execution Mode

### Overview

Code Execution Mode changes how the LLM interacts with MCP tools. Instead of receiving all MCP tools as direct JSON tool calls, the LLM gets two key tools and discovers/calls MCP tools by writing code:

1. **`get_api_spec`** (virtual tool) — Fetches OpenAPI 3.0 YAML specs for MCP server tools
2. **`execute_shell_command`** (direct tool) — Runs Python/bash code that calls per-tool HTTP endpoints

**Key Benefits:**
- **Complex Logic**: Python loops, conditionals, data transformations, multi-step pipelines
- **Tool Chaining**: Multiple MCP tool calls in a single script execution
- **Batch Operations**: Process data from multiple tools programmatically
- **Token Efficiency**: LLM only fetches specs for tools it needs (not all tool definitions upfront)

### How It Works

#### Architecture

```
LLM
 ├── Sees tool index in system prompt (JSON: server → tool names)
 ├── get_api_spec(server, tool) → OpenAPI YAML spec
 ├── execute_shell_command(python code) → calls per-tool HTTP endpoints
 │     ├── POST /tools/mcp/{server}/{tool}    (MCP tools)
 │     └── POST /tools/custom/{tool}          (custom tools, if needed)
 └── Direct tools: read_workspace_file, execute_shell_command, etc.
```

#### Workflow

1. **Tool Index** — The system prompt includes a JSON index of available MCP servers and tools
2. **Discovery** — LLM calls `get_api_spec(server_name="sonarqube", tool_name="search_issues")` to get the OpenAPI spec for a specific tool
3. **Execution** — LLM writes Python code via `execute_shell_command` that makes HTTP POST requests to per-tool endpoints
4. **Auth** — Code uses `MCP_API_URL` and `MCP_API_TOKEN` env vars (pre-set in the shell environment)

#### Tool Availability

| Tool | Type | How Accessed |
|------|------|-------------|
| MCP tools (e.g., `search_issues`) | Via HTTP API | `POST /tools/mcp/{server}/{tool}` with bearer token |
| `execute_shell_command` | Direct tool call | LLM calls it directly to run Python/bash |
| `get_api_spec` | Virtual tool call | LLM calls it to get OpenAPI specs |
| `read_workspace_file`, etc. | Direct tool call | LLM calls workspace tools directly |

### Key Files

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| **Server Integration** | `agent_go/cmd/server/server.go` | Per-tool routes, API token generation, env var setup |
| **Workspace Tools** | `agent_go/cmd/server/virtual-tools/workspace_advanced_tools.go` | `getMCPExtraEnv()`, env var forwarding |
| **OpenAPI Generator** | `mcpagent/mcpcache/openapi/generator.go` | `GenerateServerOpenAPISpec()`, `GenerateToolIndex()` |
| **Per-Tool Handlers** | `mcpagent/executor/per_tool_handler.go` | `HandlePerToolMCPRequest()`, `HandlePerToolCustomRequest()` |
| **Bearer Auth** | `mcpagent/executor/security.go` | `GenerateAPIToken()`, `AuthMiddleware()` |
| **Prompt Builder** | `mcpagent/agent/prompt/builder.go` | `GetCodeExecutionInstructions()` |

### Example: LLM Calling an MCP Tool via Python

```python
import requests, os, json

url = os.environ["MCP_API_URL"] + "/tools/mcp/sonarqube/search_issues"
headers = {
    "Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}",
    "Content-Type": "application/json"
}
resp = requests.post(url, json={
    "project_key": "my-project",
    "severities": "CRITICAL,MAJOR"
}, headers=headers)

data = resp.json()
if data.get("success"):
    issues = json.loads(data["result"])
    for issue in issues.get("issues", []):
        print(f"- {issue['severity']}: {issue['message']}")
else:
    print("Error:", data.get("error"))
```

### Security

#### Auth Separation
- **`/api/*` routes** — JWT auth (user authentication)
- **`/tools/*` routes** — Bearer token auth (code execution authentication, bypasses JWT)

#### Environment Variables
- `MCP_API_URL` — Base URL for per-tool endpoints (uses `host.docker.internal:{port}` when workspace API is Dockerized)
- `MCP_API_TOKEN` — Random 32-byte hex token, generated at startup
- Both are forwarded to shell commands via `extra_env` in the workspace API request body (only `MCP_*` prefix is whitelisted server-side)

#### Shell Environment Sanitization
- Workspace API uses `security.BuildSafeEnvironment()` — only essential vars (PATH, HOME, etc.)
- `MCP_*` vars are injected explicitly via the request body, not inherited from parent process

### Configuration

#### Frontend

In the preset modal, select **Code Exec** from the 3-way tool mode toggle (Simple / Code Exec / Tool Search).

#### Backend

```go
// Passed automatically based on preset/step config
mcpagent.WithCodeExecutionMode(true)
```

#### Multi-Agent Chat

Code execution is available as a `tool_mode` for delegated sub-agents:
```json
{"tool_mode": "code_execution"}
```
Use for data analysis, batch operations, loops over MCP tool results, or tasks that benefit from programmatic orchestration.

### Workflow-Specific Notes

In workflow mode, code execution mode works the same as in chat — the LLM uses `get_api_spec` to discover tools and writes Python code via `execute_shell_command` to call per-tool HTTP endpoints.

Key workflow-specific considerations:
- **WorkspacePath**: Pass as a variable in the Python code (e.g., via environment or hardcoded from step context)
- **Learning capture**: The specialized code execution learning agent captures Python code patterns for reuse across iterations
- **Knowledgebase**: Persistent `knowledgebase/` folder available for shared data across runs

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| `MCP_API_URL` is `None` in shell | Env vars not forwarded to Docker container | Check `extra_env` forwarding, verify `MCP_*` whitelisting in `handlers/shell.go` |
| `Connection refused` to `127.0.0.1` | Shell runs inside Docker, can't reach host | `MCP_API_URL` should use `host.docker.internal` |
| `401 Unauthorized` on per-tool endpoint | Missing/wrong bearer token | Verify `MCP_API_TOKEN` env var matches server token |
| LLM calls `get_api_spec` for custom tools | Custom tool categories in tool index | Custom tools excluded from index in code exec mode |

---