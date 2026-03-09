# Tool Search Mode

## 3. Tool Search Mode

### 📋 Overview

Tool Search Mode allows agents to discover tools on-demand instead of loading all tools upfront. This significantly reduces token usage when working with large MCP tool catalogs by only exposing tools that are relevant to the current task.

**Key Benefits:**
- **Token Efficiency**: Only load tools when needed, saving context tokens
- **Large Catalogs**: Handle hundreds of tools without overwhelming the context window
- **On-Demand Discovery**: Agents use `search_tools` to find relevant tools dynamically
- **Pre-Discovered Tools**: Critical tools can be pre-loaded and always available

### 📁 Key Files & Locations

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| **Base Orchestrator** | [`agent_go/pkg/orchestrator/base_orchestrator.go`](../agent_go/pkg/orchestrator/base_orchestrator.go) | `useToolSearchMode`, `preDiscoveredTools` fields |
| **Base Orchestrator Getters** | [`agent_go/pkg/orchestrator/base_orchestrator_getters.go`](../agent_go/pkg/orchestrator/base_orchestrator_getters.go) | `GetUseToolSearchMode()`, `GetPreDiscoveredTools()` |
| **Agent Config** | [`agent_go/pkg/orchestrator/agents/interfaces.go`](../agent_go/pkg/orchestrator/agents/interfaces.go) | `UseToolSearchMode`, `PreDiscoveredTools` in config |
| **Step Config** | [`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go) | Step-level overrides in `AgentConfigs` |
| **Controller Factory** | [`agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`](../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go) | `getToolSearchMode()`, `getPreDiscoveredTools()` |
| **Frontend Modal** | [`frontend/src/components/PresetModal.tsx`](../frontend/src/components/PresetModal.tsx) | UI Toggle and pre-discovery logic |

### 🔄 How It Works

#### Without Tool Search Mode (Default)

By default, all tools from selected MCP servers are loaded into the LLM's context:

```
Agent Context:
├── System Prompt
├── All Tools (50-100+ tools)  ← Can consume many tokens
├── Conversation History
└── User Message
```

#### With Tool Search Mode Enabled

When Tool Search Mode is enabled, the agent starts with only:
1. The `search_tools` virtual tool for discovering other tools
2. The `add_tool` virtual tool for adding discovered tools to the context
3. Any pre-discovered tools specified in configuration

```
Agent Context:
├── System Prompt
├── search_tools (virtual tool)
├── add_tool (virtual tool)
├── Pre-discovered Tools (if any)
├── Conversation History
└── User Message
```

The agent can then use `search_tools` to find relevant tools, and `add_tool` to load them:

```
Agent: "I need to read a file. Let me search for file tools."
→ search_tools("read file workspace")
← Returns: read_workspace_file, read_file, etc.

Agent: I found the tool I need.
→ add_tool(tool_names: ["read_workspace_file"])

Agent: Now I have the file reading tool available.
→ read_workspace_file(filepath: "config.json")
```

### 💻 Frontend Integration

The Frontend Preset Modal now supports Tool Search Mode directly:

1.  **3-Way Toggle**: A segmented control allows selecting one of three exclusive modes:
    - **Simple**: Direct tool access (default).
    - **Code Exec**: Tools accessed via Python/Go code generation.
    - **Tool Search**: Dynamic tool discovery with `search_tools`.

2.  **Pre-Discovered Tools Logic**:
    - When **Tool Search** mode is selected, any tools selected in the "Selected Tools" list are automatically treated as **Pre-Discovered Tools**.
    - This allows you to "lock in" essential tools (like `read_workspace_file`, `human_feedback`) so they are always available, while letting the agent search for specialized tools on demand.
    - The UI automatically handles the mapping: `selected_tools` in UI -> `pre_discovered_tools` in backend request.

### ⚙️ Configuration

#### Preset-Level Configuration

Tool Search Mode can be enabled at the preset level in the database:

```json
{
  "use_tool_search_mode": true,
  "pre_discovered_tools": ["read_workspace_file", "write_workspace_file"]
}
```

#### Step-Level Configuration

Override at the step level in `step_config.json`:

```json
{
  "steps": [
    {
      "id": "step-1",
      "title": "Data Collection",
      "agent_configs": {
        "use_tool_search_mode": true,
        "pre_discovered_tools": ["read_workspace_file", "write_workspace_file"],
        "selected_servers": ["filesystem", "database", "api_server"]
      }
    },
    {
      "id": "step-2",
      "title": "Analysis",
      "agent_configs": {
        "use_tool_search_mode": false
      }
    }
  ]
}
```

#### Configuration Priority

The configuration follows a cascading priority:

1. **Step Config** (highest priority) - If `use_tool_search_mode` is set in step config
2. **Preset/Orchestrator Default** - Falls back to orchestrator-level setting
3. **System Default** - `false` (tool search mode disabled)

### 📈 Optimization Strategy

The Plan Tool Optimization Agent supports a specific strategy for Tool Search Mode:

**Strategy: Convert to Tool Search Mode**
If the optimizer identifies a clear set of tools used successfully in previous runs (via Learnings or Logs), it may recommend converting the step to Tool Search Mode:

1.  **Enable Tool Search Mode**: Set `use_tool_search_mode: true`.
2.  **Lock In Known Tools**: Set `pre_discovered_tools` to the list of successfully used tools.
3.  **Allow Expansion**: Set `selected_tools` to `['server:*']` (all tools from relevant servers) to allow the agent to search for *other* tools if needed, without loading them all into context initially.

This strategy combines the efficiency of a static tool list (known tools are ready) with the flexibility of dynamic search (unknown tools can be found).

### 🛠️ Pre-Discovered Tools

Pre-discovered tools are tools that are always available without needing to search. Use these for:

- **Critical Tools**: Tools the agent needs immediately (e.g., workspace file operations)
- **Frequently Used**: Tools used in almost every interaction
- **Foundation Tools**: Base tools that other operations depend on

#### Example Configuration

```json
{
  "use_tool_search_mode": true,
  "pre_discovered_tools": [
    "read_workspace_file",
    "write_workspace_file",
    "list_workspace_files",
    "human_feedback"
  ]
}
```

#### Tool Name Format

Pre-discovered tools should be specified as tool names (not `server:tool` format):

- **Correct**: `"read_workspace_file"`, `"aws_cli_query"`
- **Incorrect**: `"filesystem:read_workspace_file"`

### ⚠️ Troubleshooting

#### Agent Can't Find Tools

**Symptom**: Agent says it doesn't have access to a tool.

**Solutions**:
1. Add the tool to `pre_discovered_tools`
2. Ensure the tool's server is in `selected_servers`
3. Check that `search_tools` is working correctly

#### High Latency

**Symptom**: Workflow is slower with Tool Search Mode.

**Solutions**:
1. Add frequently-used tools to `pre_discovered_tools`
2. Consider disabling for steps with known tool requirements
3. Evaluate if the token savings justify the latency

#### Tools Not Appearing in Search

**Symptom**: `search_tools` doesn't return expected tools.

**Solutions**:
1. Verify the tool's server is in `selected_servers`
2. Check tool naming and description for searchability
3. Ensure MCP server is connected and healthy

---