# Tool Search Mode

## Overview

Tool Search Mode is an agent-level feature that enables LLMs to work with large tool catalogs efficiently. Instead of loading all tools upfront, the agent exposes a `search_tools` function that allows the LLM to discover and load tools on-demand.

**Key Benefits:**
- **Context Efficiency**: Only loaded tools consume context tokens
- **Cross-LLM Support**: Works with any LLM provider (Anthropic, OpenAI, etc.)
- **Dynamic Discovery**: Tools are discovered and loaded during conversation
- **Pattern Matching**: Regex-based search for flexible tool discovery

---

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│                        Tool Search Flow                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. Initial State                                               │
│     ┌──────────────────────────┐                                │
│     │ LLM sees:                │                                │
│     │ - search_tools           │  ← discovery tools             │
│     │ - add_tool               │                                │
│     │ - show_all_tools         │                                │
│     │ - search_large_output    │  ← context offloading          │
│     │ - (pre-discovered tools) │  ← if configured               │
│     └──────────────────────────┘                                │
│                                                                 │
│  2. User asks: "Get weather in Tokyo"                           │
│                                                                 │
│  3. LLM calls: search_tools(query: "weather")                   │
│     ┌─────────────────────────────────────────┐                 │
│     │ Agent searches all deferred tools       │                 │
│     │ Returns: get_weather, weather_forecast  │                 │
│     │ (Tools are NOT added to context yet)    │                 │
│     └─────────────────────────────────────────┘                 │
│                                                                 │
│  4. LLM calls: add_tool(tool_names: ["get_weather"])            │
│                                                                 │
│  5. After add_tool returns                                      │
│     ┌─────────────────────────┐                                 │
│     │ LLM now sees:           │                                 │
│     │ - search_tools          │                                 │
│     │ - add_tool              │                                 │
│     │ - search_large_output   │                                 │
│     │ - get_weather           │  ← newly added                  │
│     └─────────────────────────┘                                 │
│                                                                 │
│  6. LLM calls: get_weather(location: "Tokyo")                   │
│                                                                 │
│  7. Agent executes tool, returns result                         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Usage

### Enabling Tool Search Mode

```go
agent, err := mcpagent.NewAgent(ctx, llm, configPath,
    mcpagent.WithToolSearchMode(true),
)
```

### With Pre-Discovered Tools

Pre-discovered tools are always available without needing to search:

```go
agent, err := mcpagent.NewAgent(ctx, llm, configPath,
    mcpagent.WithToolSearchMode(true),
    mcpagent.WithPreDiscoveredTools([]string{
        "get_weather",      // Always available
        "send_message",     // Always available
        "list_files",       // Always available
    }),
)
```

### Pre-Discovering Workspace Tools

For workflows that need workspace tools immediately available:

```go
agent, err := mcpagent.NewAgent(ctx, llm, configPath,
    mcpagent.WithToolSearchMode(true),
    mcpagent.WithPreDiscoveredTools([]string{
        "read_workspace_file",   // Workspace tools immediately available
        "list_workspace_files",
        "write_workspace_file",
    }),
)
```

**Note:** Workspace tools not in the pre-discovered list can still be found via `search_tools(query: "workspace")` and added with `add_tool`.

### Configuration Options

```go
// Enable tool search mode
mcpagent.WithToolSearchMode(true)

// Pre-discover specific tools (always available without searching)
mcpagent.WithPreDiscoveredTools([]string{"tool1", "tool2"})
```

---

## Key Files & Locations

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| **Agent Core** | `agent/agent.go` | `WithToolSearchMode()`, `RegisterCustomTool()`, `getToolsForToolSearchMode()` |
| **Tool Search Handlers** | `agent/tool_search_handlers.go` | `handleSearchTools()`, `handleAddTool()`, `initializeToolSearch()` |
| **Tool Definitions** | `agent/virtual_tools_definitions.go` | `CreateToolSearchTools()` |
| **Virtual Tools** | `agent/virtual_tools.go` | `CreateVirtualTools()`, `executeVirtualTool()` |
| **Conversation Loop** | `agent/conversation.go` | Tool refresh after `add_tool` |
| **Prompt Builder** | `agent/prompt/builder.go` | `GetToolSearchInstructions()` |

---

## The search_tools Function

### Definition

```json
{
  "name": "search_tools",
  "description": "Search for available tools by name or description. Returns matching tools that must be added using add_tool.",
  "parameters": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Regex pattern to search tool names and descriptions"
      }
    },
    "required": ["query"]
  }
}
```

### Search Patterns

| Pattern | Matches |
|---------|---------|
| `weather` | Tools with "weather" in name or description |
| `database.*query` | Tools matching "database" followed by "query" |
| `(?i)slack` | Case-insensitive match for "slack" |
| `get_.*` | All tools starting with "get_" |
| `file\|folder` | Tools matching "file" OR "folder" |

### Response Format

```json
{
  "found": 2,
  "tools": [
    {
      "name": "get_weather",
      "description": "Get current weather for a location"
    },
    {
      "name": "weather_forecast",
      "description": "Get weather forecast for the next 7 days"
    }
  ],
  "message": "Found matching tools. Use 'add_tool' to load the ones you need."
}
```

### Fuzzy Search Fallback

When regex search finds no matches, the agent automatically performs fuzzy search:

1. **Substring matching**: Checks if query is contained in tool name/description
2. **Word-level matching**: Matches individual words from the query
3. **Character-level similarity**: Falls back to character overlap scoring

Returns up to 5 best matches sorted by relevance score.

**Example:**
- Query: `"wether"` (typo) → Still finds `get_weather` via fuzzy match
- Query: `"send msg"` → Matches `send_message` via word matching

---

## The add_tool Function

### Definition

```json
{
  "name": "add_tool",
  "description": "Add one or more tools to your available tools. Use this after finding tools with search_tools.",
  "parameters": {
    "type": "object",
    "properties": {
      "tool_names": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Array of exact names of the tools to add (e.g., ['read_file', 'weather_get'])."
      }
    },
    "required": ["tool_names"]
  }
}
```

---

## The show_all_tools Function

### Definition

```json
{
  "name": "show_all_tools",
  "description": "List all available tool names. Returns names only - use search_tools with a tool name to get its description.",
  "parameters": {
    "type": "object",
    "properties": {}
  }
}
```

### Response Format

```json
{
  "total": 45,
  "tools": [
    "add_tool",
    "create_issue",
    "get_weather",
    "list_files",
    "read_file",
    "search_tools",
    "send_message"
  ]
}
```

### Use Cases

- **Quick catalog overview**: See all available tools at a glance without loading full descriptions
- **Tool name discovery**: Find exact tool names for use with `search_tools` or `add_tool`
- **Debugging**: Verify which tools are available in the current session

---

## Immediately Available Tools

When Tool Search Mode is enabled, certain tools are **always immediately available** without needing to search or add them:

### Discovery Tools
| Tool | Purpose |
|------|---------|
| `search_tools` | Search for tools by name or description |
| `add_tool` | Add discovered tools to your toolkit |
| `show_all_tools` | List all available tool names |

### Context Offloading Tools
These tools are always available because they're needed when large tool outputs are offloaded:

| Tool | Purpose |
|------|---------|
| `search_large_output` | Unified tool for accessing offloaded files (read/search/query) |
| `read_large_output` | Read character ranges from offloaded files |
| `query_large_output` | Execute jq queries on offloaded JSON files |

### Pre-Discovered Tools
Tools specified in `WithPreDiscoveredTools()` are immediately available.

### Special Category Tools
Custom tools with these categories are always immediately available:
- `structured_output` - Orchestration/control tools
- `human` - Human feedback tools (require event bridge for UI)

---

## Discoverable Tools

The following tool types are **deferred** and must be discovered via `search_tools` + `add_tool`:

| Tool Type | Examples |
|-----------|----------|
| **MCP Server Tools** | `get_weather`, `list_files`, `create_issue` |
| **Custom/Workspace Tools** | `read_workspace_file`, `list_workspace_files`, `write_workspace_file` |
| **Virtual Tools** | `get_prompt`, `get_resource` |

**Note:** Custom tools (including workspace tools) registered via `RegisterCustomTool()` are now fully discoverable via `search_tools`. If a custom tool is in the `pre_discovered_tools` list, it will be immediately available instead.

---

## Tool Filtering Integration

Tool Search Mode **respects tool filtering** configured via `selectedTools` and `selectedServers`. When filtering is active:

1. **Only filtered tools are discoverable** - Tools that don't pass the filter are excluded from `allDeferredTools`
2. **Filtered tools cannot be discovered** - `search_tools` will not return tools that are filtered out
3. **Special categories bypass filtering** - `structured_output` and `human` category tools are always available

### Example with Filtering

```go
agent, err := mcpagent.NewAgent(ctx, llm, configPath,
    mcpagent.WithToolSearchMode(true),
    mcpagent.WithSelectedServers([]string{"github", "slack"}),  // Only these servers' tools
    mcpagent.WithSelectedTools([]string{"workspace:read_workspace_file"}),  // Only this workspace tool
)
```

In this example:
- Only tools from `github` and `slack` servers are discoverable
- Only `read_workspace_file` from workspace tools is discoverable
- Other workspace tools like `write_workspace_file` are NOT discoverable

### Filtering Behavior

| Configuration | Behavior |
|--------------|----------|
| No filtering | All tools are discoverable |
| `selectedServers` only | All tools from selected servers are discoverable |
| `selectedTools` only | Only specific tools in the list are discoverable |
| `selectedServers` + `selectedTools` | Tools matching either are discoverable |
| `server:*` pattern | All tools from that server are discoverable |

---

## Comparison with Other Modes

| Aspect | Normal Mode | Code Execution Mode | Tool Search Mode |
|--------|-------------|---------------------|------------------|
| **Tools visible** | All tools | `discover_code_files`, `write_code` | Discovery tools + context offloading + pre-discovered + discovered |
| **Tool calling** | Direct JSON | Go code | Direct JSON |
| **Discovery method** | N/A | Via Go packages | Via regex search |
| **Context usage** | High (all tools) | Low (2 tools) | Dynamic (grows as tools are added) |
| **Best for** | <30 tools | Complex logic, chaining | Large tool catalogs |

---

## System Prompt

When tool search mode is enabled, the system prompt includes:

```markdown
## Tool Search Mode

You have access to a large catalog of tools, but they are not loaded by default.
Use the search_tools function to discover tools, and then add_tool to load them.

**How to search & add:**
- Call search_tools with a regex pattern
- Review returned tools
- Call add_tool with the names of the tools you want to use
- Use the discovered tools to complete your task

**Available tool categories:**
- google-sheets (spreadsheet operations)
- github (repository management)
- slack (messaging)

**Workflow:**
1. Understand what the user needs
2. Search for relevant tools using search_tools
3. Add the tools you need using add_tool(tool_names=["..."])
4. Use the discovered tools to complete the task
```

---

## Implementation Details

### Agent State

```go
type Agent struct {
    // ... existing fields

    UseToolSearchMode  bool                      // Enable tool search mode
    discoveredTools    map[string]llmtypes.Tool  // Tools discovered this session
    allDeferredTools   []llmtypes.Tool           // All available tools (hidden until discovered)
    preDiscoveredTools []string                  // Tool names always available without searching
}
```

### Tool Discovery Flow

1. **Initialization**: MCP tools, virtual tools, and custom tools stored in `allDeferredTools`
2. **Pre-Discovery**: Tools in `preDiscoveredTools` list are added to `discoveredTools`
3. **Search**: `handleSearchTools()` matches query against `allDeferredTools`
4. **Add**: `handleAddTool()` moves tools from deferred to `discoveredTools`
5. **Refresh**: `getToolsForToolSearchMode()` returns discovery tools + context offloading + discovered tools
6. **Execution**: LLM can call discovered tools (execution functions stored separately)

### Tool Initialization

When tool search mode is enabled, tools are categorized:

```go
// During agent initialization:

// 1. MCP tools → allDeferredTools (require discovery)
for _, tools := range ag.mcpServerTools {
    ag.allDeferredTools = append(ag.allDeferredTools, tools...)
}

// 2. Virtual tools (context offloading tools are immediately available)
for _, tool := range virtualTools {
    if isContextOffloadingTool(tool) {
        // search_large_output, read_large_output, query_large_output
        // Always immediately available
        filteredVirtualTools = append(filteredVirtualTools, tool)
    } else {
        ag.allDeferredTools = append(ag.allDeferredTools, tool)
    }
}

// 3. Tool search tools always available
filteredVirtualTools = append(filteredVirtualTools, CreateToolSearchTools()...)
```

### Custom Tool Registration

Custom tools (workspace, human, etc.) registered via `RegisterCustomTool()` are handled specially:

```go
func (a *Agent) RegisterCustomTool(name, description string, ...) {
    if a.UseToolSearchMode {
        // Check if pre-discovered or special category
        isPreDiscovered := contains(a.preDiscoveredTools, name)
        isSpecialCategory := category == "structured_output" || category == "human"

        if isPreDiscovered || isSpecialCategory {
            // Immediately available
            a.discoveredTools[name] = tool
            a.allDeferredTools = append(a.allDeferredTools, tool)
            a.filteredTools = a.getToolsForToolSearchMode()
        } else {
            // Requires discovery via search_tools + add_tool
            a.allDeferredTools = append(a.allDeferredTools, tool)
        }
    }
    // ... rest of registration (execution function always stored in customTools)
}
```

**Key Point:** The execution function is always stored in `customTools` map regardless of discovery state. The `discoveredTools` map only controls which tools the LLM can see/call.

---

## Best Practices

### When to Use Tool Search Mode

**Good use cases:**
- 30+ tools available
- Multiple MCP servers connected
- Tools from different domains (slack, github, database, etc.)
- User requests vary widely

**When normal mode is better:**
- Less than 30 tools
- All tools frequently used
- Simple, focused use cases

### Optimizing Tool Descriptions

Good tool descriptions improve search accuracy:

```go
// Good - searchable keywords
"Get current weather conditions for a city or location"

// Bad - vague
"Returns data about conditions"
```

### Guiding the LLM

Include tool categories in the system prompt so the LLM knows what to search for:

```go
categories := []string{
    "google-sheets (spreadsheet operations)",
    "github (repository and PR management)",
    "slack (messaging and channels)",
}
prompt := GetToolSearchInstructions(categories)
```

---

## Error Handling

### Invalid Regex Pattern

```json
{
  "error": "invalid regex pattern: missing closing )"
}
```

### No Matches Found

```json
{
  "found": 0,
  "tools": [],
  "message": "No tools found matching the pattern"
}
```

---

## Example

See the complete working example at [`examples/tool_search/`](../examples/tool_search/):

```bash
cd examples/tool_search
export $(grep -v '^#' ../../.env | xargs)
go run main.go
```

The example demonstrates:
- Enabling tool search mode with `WithToolSearchMode(true)`
- LLM discovering documentation tools via `search_tools`
- Using discovered tools to get React library information

---

## Related Documentation

- [`docs/code_execution_agent.md`](./code_execution_agent.md) - Code execution mode
- [`examples/tool_search/`](../examples/tool_search/) - Complete working example
- [`agent/agent.go`](../agent/agent.go) - Agent implementation
- [`agent/prompt/builder.go`](../agent/prompt/builder.go) - System prompt construction
