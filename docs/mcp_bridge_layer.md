# 🌉 MCP Bridge Layer & Exposed APIs

The **MCP Bridge Layer** is a critical architectural component of the MCP Agent Builder. It acts as a universal proxy and translation layer, allowing external local CLI agents (like **Claude Code** and **Gemini CLI**) to seamlessly access the orchestrator's entire suite of loaded tools, virtual tools, and workspace capabilities.

## Architecture Overview

Instead of requiring every agent to manage its own MCP server connections, the orchestrator centralizes tool discovery, session management, and routing. External agents use a lightweight proxy binary called `mcpbridge` to communicate with the orchestrator's REST API.

1. **Local CLI Agent** (e.g., `claude-code`, `gemini`) spawns `mcpbridge`.
2. **mcpbridge** translates the stdio-based MCP protocol into HTTP REST calls.
3. **Orchestrator** processes the HTTP request, executes the tool (routing to Docker, external APIs, or the local Workspace), and returns the result.

---

## 🔌 Exposed APIs

The orchestrator exposes several API layers to facilitate this bridge and manage the workspace.

### 1. Tool Execution & Discovery (Bridge API)
These endpoints are consumed by `mcpbridge` and the web UI to interact with tools.

*   **`POST /api/mcp/execute`**
    *   The central routing endpoint. Accepts a JSON payload containing the `server_name`, `tool_name`, and `arguments`.
    *   Automatically routes the request to the correct underlying MCP server, virtual tool, or workspace capability.
*   **`GET /api/tools`**
    *   Returns a flattened, formatted schema of all available tools across all connected MCP servers and virtual tools.
*   **`GET /api/mcp-config` & `POST /api/mcp-config`**
    *   Retrieves and updates the active MCP server configuration (`mcp_servers.json`).
*   **`POST /api/mcp-config/discover`**
    *   Forces a manual discovery and reconnection to all configured MCP servers.

### 2. Workspace API (Port 8080)
The `workspace` container runs a dedicated Planner API that the orchestrator interfaces with for local file and system management.

*   **Document Management:**
    *   `GET /api/documents` / `POST /api/documents`
    *   `GET/PUT/DELETE /api/documents/*filepath`
    *   `GET /api/glob` (File discovery via glob patterns)
*   **Search & Semantic Intelligence:**
    *   `GET /api/search` (Lexical grep search)
    *   `GET /api/search/semantic` (Vector search via Qdrant)
    *   `POST /api/search/process-file`
*   **System & Execution:**
    *   `POST /api/execute` (Secure shell command execution within the workspace)
    *   `POST /api/upload` (File uploads)
*   **Google Workspace (GWS):**
    *   `GET /api/gws-auth-status`
    *   `POST /api/gws-sync-skills`

---

## 🌎 Universal Stdio Gateway

The `mcpbridge` binary isn't just for Claude or Gemini—it serves as a **Universal Stdio Gateway**. 

Any external LLM agent, framework (LangChain, AutoGPT, CrewAI), or custom script that supports the Model Context Protocol (MCP) via `stdio` can use `mcpbridge` to access your entire tool ecosystem through a single connection.

### Why use the Centralized Bridge?
*   **Single Connection, 100+ Tools**: Connect to one "server" (`mcpbridge`) and immediately gain access to every tool configured in your orchestrator (GWS, Slack, Workspace, Browser, etc.).
*   **Centralized Auth**: Manage API keys and OAuth tokens in one place (the orchestrator) rather than in every individual agent script.
*   **Session Isolation**: Use the `MCP_SESSION_ID` environment variable to isolate tool execution and folder guards between different external agents.
*   **Monitoring**: Every tool call made via the bridge is logged and visible in the orchestrator's central dashboard.

### Configuration for External Frameworks
To connect an external agent, simply point it to the `mcpbridge` binary:

```bash
# Example: Launching a custom MCP-compatible agent
my-agent-cli --mcp-command "mcpbridge" --mcp-env "MCP_API_URL=http://localhost:8080/api"
```