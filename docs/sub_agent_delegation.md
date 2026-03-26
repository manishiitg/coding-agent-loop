# Sub-Agent Delegation System

## Overview

Sub-agent delegation allows the main agent to spawn independent sub-agents to handle tasks. Two modes are supported:

1. **Simple delegation** (`delegate` tool) — Quick one-off tasks via `/spawn` in Chat mode
2. **Plan-driven delegation** (`create_delegation_plan` + `delegate`) — Complex multi-step projects with task tracking via plan.md, available in **Multi Agent Chat** mode

When delegation is enabled, the agent receives delegation tools and can spawn sub-agents with the same tool access as the parent (workspace, browser, skills, MCP servers).

---

## Quick Start

### Multi Agent Chat Mode (Plan-Driven Delegation)

**Multi Agent Chat** is a top-level mode alongside Chat and Workflow. It provides plan-driven delegation with multi-LLM tiers — always on, no slash command needed.

- Select **Multi Agent Chat** from the mode switcher or mode selection modal
- Auto tool mode chip + tier config chip appear automatically in the chat input
- Plans are stored in `Chats/{planID}/plan.md` (visible in the workspace sidebar)
- The `Chats/` folder is a **per-user folder** (auto-created, isolated under `_users/{userID}/Chats/`)

### Simple Delegation (Chat Mode)

In Chat mode, simple delegation can be toggled via slash commands:

| Command | Action |
|---------|--------|
| `/spawn` | Enable simple sub-agent delegation |
| `/nospawn` | Disable sub-agent delegation |

When spawn is enabled, a small purple **GitBranch icon** appears next to the send button.

---

## Delegation Tools

Only two tools — `delegate` and `create_delegation_plan`. The manager reads/updates plan.md via workspace tools.

### 1. `delegate` — Execute a Task

Spawns a sub-agent with a single instruction. Used for both one-off tasks and executing plan tasks.

```json
{
  "name": "delegate",
  "parameters": {
    "instruction": "Clear, detailed instructions for the sub-agent",
    "reasoning_level": "high | medium | low (required in Multi Agent Chat)",
    "tool_mode": "simple | code_execution | tool_search (optional — default: simple)",
    "plan_folder": "Chats/{planID} (optional — restricts write access)"
  }
}
```

#### Tool Mode Options

| Mode | When to Use |
|------|------------|
| **`simple`** (default) | Most tasks, including writing Python/Bash scripts via shell tools |
| **`code_execution`** | Worker writes Python code to call MCP tools via HTTP API. Best for data analysis, batch operations, loops over MCP tool results, or programmatic orchestration of multiple tool calls |
| **`tool_search`** | When 3+ MCP servers are available and the agent needs to discover tools on-demand |

**Guideline**: Use `code_execution` when the task involves fetching/processing data from MCP servers programmatically (aggregation, filtering, multi-step data pipelines). Use `simple` for file operations, script writing, and general tasks.
```

### 2. `create_delegation_plan` — Create a Plan

Spawns a planner sub-agent that analyzes the objective and writes a `plan.md` todo list to `Chats/{planID}/`. The planner has workspace tools for **research only** (reading files, querying data, reading skill instructions) — it does NOT execute plan steps.

```json
{
  "name": "create_delegation_plan",
  "parameters": {
    "objective": "What needs to be accomplished",
    "context": "Optional additional context (optional)",
    "reasoning_level": "high | medium | low (optional — LLM decides)"
  }
}
```

**Returns**: `plan_id`, `plan_folder`, `plan_file` path, `plan_content` (the full plan.md content), and the planner's output. The plan content is returned directly so the manager doesn't need to read it separately.

### Plan Management (via workspace tools)

After creating a plan, the manager agent reads and updates `plan.md` directly:

- **Read plan**: Use `read_workspace_file` with filepath `Chats/{plan_id}/plan.md` (workspace API handles per-user path resolution)
- **Check status**: Re-read plan.md and check checkboxes
- **Mark complete**: `execute_shell_command("sed -i 's/- \\[ \\] \\*\\*task-N\\*\\*/- [x] **task-N**/' Chats/{plan_id}/plan.md")`
- **Add notes**: `create_or_update_workspace_file` to append results

**Note**: Use `read_workspace_file` instead of shell `cat` for reading plan files. The workspace API handles per-user path resolution correctly, while shell commands go through mount namespace isolation which may not always resolve per-user paths.

---

## Planner Capabilities Context

When `create_delegation_plan` runs, the planner sub-agent receives a `CapabilitiesContext` describing available tools:

| Field | Description |
|-------|-------------|
| `EnabledServers` | MCP servers available to workers |
| `SelectedTools` | Specifically enabled tools (`server:tool` format) |
| `Skills` | Skill summaries (name, description, folder) |
| `HasWorkspace` | Whether workspace file access is available |
| `HasBrowser` | Whether browser automation is available |

The planner uses this information to reference specific servers, tools, and skills in task descriptions. It reads skill files (e.g., `cat skills/<name>/SKILL.md`) for research before writing the plan.

---

## Plan File Format

Plans are stored as a single `plan.md` file in `Chats/{planID}/`:

```markdown
# Plan: Short title

## Strategy
High-level approach description

## Tasks

- [ ] **task-1**: Task title
  - Detailed self-contained description
  - Reasoning level: high

- [ ] **task-2**: Task title
  - Detailed description
  - Depends on: task-1
  - Reasoning level: medium

- [x] **task-3**: Completed task
  - Description
  - Reasoning level: low
```

The workspace API routes `Chats/` to `_users/{userID}/Chats/` via per-user isolation.

---

## Multi-LLM Reasoning Tiers

The `reasoning_level` parameter is **optional** on both `delegate` and `create_delegation_plan`. The LLM decides the appropriate tier based on task complexity. If not specified, the parent model is used as fallback.

| Tier | Use Case | Default |
|------|----------|---------|
| **high** | Complex reasoning, architecture decisions | Parent model (fallback) |
| **medium** | Standard coding, implementation | Parent model (fallback) |
| **low** | Simple tasks, formatting, lookups | Parent model (fallback) |

**Validation**: Only `"high"`, `"medium"`, `"low"` are accepted. Invalid values (e.g., LLM hallucinations like `"highbinary"`) are silently ignored and the parent model is used instead.

### Tier Configuration

**Priority order**: Frontend config > Environment variables > Parent model (fallback)

**Frontend**: In Multi Agent Chat mode, configure tiers via the tier config chip in the chat input, or the **Delegation Models** section in the left sidebar. Sent as `delegationTierConfig` in the chat request:
```json
{
  "delegationTierConfig": {
    "high": { "provider": "anthropic", "model_id": "claude-sonnet-4-20250514" },
    "medium": { "provider": "google", "model_id": "gemini-2.0-flash" },
    "low": { "provider": "google", "model_id": "gemini-2.0-flash-lite" }
  }
}
```

**Environment variables**: `DELEGATION_HIGH_PROVIDER`, `DELEGATION_HIGH_MODEL`, etc.

---

## Sub-Agent Configuration

### Inherited from Parent

Sub-agents inherit ALL configuration from the parent agent request:

| Setting | Source | Fallback |
|---------|--------|----------|
| **Temperature** | Parent request | 0.7 |
| **MaxTurns** | Parent request | 100 |
| **Timeout** | None (parent context controls lifetime) | — |
| **ToolTimeout** | `TOOL_TIMEOUT_SECONDS` env var | 5 minutes |
| **Summarization** | All 6 parent fields | Parent defaults |
| **Context Editing** | All 3 parent fields | Parent defaults |
| **LargeOutputThreshold** | `LARGE_OUTPUT_THRESHOLD` env var | Default |

**Summarization fields**: `EnableContextSummarization`, `SummarizeOnTokenThreshold`, `TokenThresholdPercent`, `SummarizeOnFixedTokenThreshold`, `FixedTokenThreshold`, `SummaryKeepLastMessages`

**Context editing fields**: `EnableContextEditing`, `ContextEditingThreshold`, `ContextEditingTurnThreshold`

### Tool Capabilities

- **Workspace tools**: Read/write workspace files (with FolderGuard restrictions)
- **Browser tools**: If enabled in parent session
- **MCP servers**: Same server connections as parent
- **Skills**: Selected skills are passed to sub-agent system prompt via `buildSkillPrompt()`

#### Code Execution Mode Sub-Agents

When `tool_mode: "code_execution"` is set on a delegate call, the sub-agent:
- Gets `get_api_spec` (virtual tool) + `execute_shell_command` (direct tool)
- MCP tools are **excluded** from direct tool list — accessed via HTTP API instead
- Custom tools (workspace_advanced, workspace_basic, etc.) remain as direct tools
- `MCP_API_URL` and `MCP_API_TOKEN` env vars are available in the shell environment
- Sub-agent writes Python code to call per-tool endpoints: `POST /tools/mcp/{server}/{tool}`

### Per-Tool Timeouts

Delegation tools (`delegate`, `create_delegation_plan`) use `RegisterCustomToolWithTimeout(..., 0)` — **no timeout** (they run indefinitely, lifetime controlled by parent context). All other tools use the global `ToolTimeout` (default 5 minutes from env).

### FolderGuard Restrictions

| Mode | Allowed Write Folders |
|------|----------------------|
| **Simple delegate** | `Chats/` (chat mode) |
| **Plan task (via PlanFolderKey)** | `Chats/{planID}/` only |
| **With skill-creator** | Adds `skills/custom/` to allowed list |

All modes block `_users/` directory access. Per-user isolation is handled by the workspace API via `X-User-ID` header.

### Isolation

- **No parent context**: Sub-agents start fresh with no access to parent conversation
- **No sub-delegation**: Sub-agents cannot spawn their own sub-agents (prevents runaway chains)
- **Max depth**: 3 levels of delegation depth
- **Same session**: All events flow to the same session ID (tagged with delegation metadata)

---

## Shell Command Error Handling

The `execute_shell_command` tool returns a **tool call error** (not success) when:
- The command exits with a non-zero exit code
- The workspace API returns an error

This ensures the LLM sees command failures as errors and can respond appropriately (retry, try a different approach, etc.).

Implementation: `formatShellResponse()` in `execute_shell_command.go` extracts `exit_code` from the response data. If non-zero, the response is returned as `fmt.Errorf()` instead of a success value.

---

## Event System

### Delegation Events

Sub-agent lifecycle emits two events:

**`delegation_start`**: Emitted when a sub-agent is spawned
```json
{
  "type": "delegation_start",
  "data": {
    "delegation_id": "delegation-0-1234567890",
    "depth": 0,
    "instruction": "Task instruction...",
    "reasoning_level": "high",
    "model_id": "claude-sonnet-4-20250514"
  }
}
```

**`delegation_end`**: Emitted when a sub-agent completes
```json
{
  "type": "delegation_end",
  "data": {
    "delegation_id": "delegation-0-1234567890",
    "result": "Task completed successfully...",
    "input_tokens": 15234,
    "output_tokens": 3456,
    "tool_calls": 12,
    "duration": "45.2s"
  }
}
```

### Sub-Agent Event Tagging

All events from a sub-agent are tagged by `DelegationEventObserver`:

```json
{
  "component": "delegation-0",
  "hierarchy_level": 1,
  "correlation_id": "delegation-0-1234567890",
  "parent_id": "session_delegation_start_delegation-0-1234567890"
}
```

### Plan File Events

When plans are created, a `workspace_file_operation` event is emitted via `PlanEventEmitter`. This triggers the workspace sidebar to highlight the plan file.

---

## Frontend UI

### Mode Integration

| Mode | Delegation | Tier Config | Workspace Sidebar |
|------|-----------|-------------|-------------------|
| **Multi Agent Chat** | Plan-driven (always on) | Tier chip in chat input + sidebar section | Shows `Chats/` and `skills/` |
| **Chat** | Simple via `/spawn` | LLM dropdown (no tiers) | Shows `Chats/` and `skills/` |
| **Workflow** | Not available | N/A | Shows workflow folder |

### Delegation Start Event
- Shows instruction summary (truncated to 80 chars)
- **+/-** expand indicator to toggle full details
- **Reasoning level badge**: Colored by tier (red=high, yellow=medium, green=low)
- **Live stats**: Token count and tool call count updated in real-time from child events
- Expanded view shows: full instruction, reasoning level, model ID, depth, delegation ID

### Delegation End Event
- Shows success/failure with summary
- **+/-** expand indicator for full details
- Inline stats: total tokens, tool calls, duration
- Expanded view shows: full result/error text, detailed token breakdown

### Live Stats Computation

`EventHierarchy.tsx` computes a `delegationStats` map by scanning all events with matching `correlation_id`:
- Counts `tool_call_start` events for tool call tally
- Sums `token_usage` events for token counts
- Passed to `EventDispatcher` as `delegationStats` prop

---

## Key Files

| Component | File Path | Description |
|-----------|-----------|-------------|
| **Delegation Tools** | `agent_go/cmd/server/virtual-tools/delegation_tools.go` | Tool definitions (`delegate`, `create_delegation_plan`), `buildPlannerPrompt()`, `CapabilitiesContext` |
| **Server Integration** | `agent_go/cmd/server/server.go` | `executeDelegatedTask()`, `buildCapabilitiesContext()`, folder guards, event emission |
| **Shell Command** | `agent_go/pkg/workspace/execute_shell_command.go` | Shell execution client with error handling (non-zero exit = tool error) |
| **Event Observer** | `agent_go/internal/events/event_observer.go` | `DelegationEventObserver` — tags sub-agent events |
| **Event Store** | `agent_go/internal/events/event_store.go` | `DelegationStartEventData`, `DelegationEndEventData` structs |
| **Agent Metrics** | `agent_go/pkg/agentwrapper/llm_agent.go` | `GetMetricsSnapshot()` for post-invoke token/tool stats |
| **Auth Middleware** | `agent_go/cmd/server/auth_middleware.go` | `GetDefaultUserID()` — returns `"default"` (aligned with workspace API) |
| **Filesystem Isolator** | `workspace/security/isolator.go` | Mount namespace (Linux) / sandbox-exec (macOS) isolation |
| **Shell Handler** | `workspace/handlers/shell.go` | Workspace API shell handler with FolderGuard + WritePathMappings |
| **Frontend Events** | `frontend/src/components/events/EventDispatcher.tsx` | Delegation event rendering with expand/collapse |
| **Event Hierarchy** | `frontend/src/components/events/EventHierarchy.tsx` | Live `delegationStats` computation from child events |
| **Frontend Toggle** | `frontend/src/components/ChatInput.tsx` | `/spawn`, `/nospawn` handlers; `effectiveDelegationMode` for multi-agent |
| **Mode Store** | `frontend/src/stores/useModeStore.ts` | `ModeCategory` type (`'chat' | 'workflow' | 'multi-agent' | null`) |
| **App Store** | `frontend/src/stores/useAppStore.ts` | `delegationMode` (`'off' | 'spawn'`) — plan is implicit in multi-agent mode |
| **Mode Selection** | `frontend/src/components/ModeSelectionModal.tsx` | 3-mode selection (Chat, Multi Agent Chat, Workflow) |
| **Workspace Sidebar** | `frontend/src/components/Workspace.tsx` | `Chats/` folder filtering for multi-agent mode |

---

## Context Keys

Defined in `delegation_tools.go`:

| Key | Type | Purpose |
|-----|------|---------|
| `ExecuteDelegatedTaskKey` | `ExecuteDelegatedTaskFunc` | Function to spawn sub-agents |
| `DelegationDepthKey` | `int` | Current delegation depth |
| `WorkspaceClientKey` | `*workspace.Client` | Workspace client for plan file I/O |
| `DelegationTierConfigKey` | `*DelegationTierConfig` | Multi-LLM tier configuration |
| `ReasoningLevelKey` | `string` | Reasoning level for current delegation |
| `PlanEventEmitterKey` | `PlanEventEmitter` | Emits workspace file events for plan files |
| `PlanFolderKey` | `string` | Plan-specific output folder (e.g., `Chats/{planID}`) |
| `CapabilitiesContextKey` | `*CapabilitiesContext` | Available MCP servers, tools, skills for planner |

---

## Limitations

1. **No sub-agent delegation**: Sub-agents cannot spawn their own sub-agents
2. **No conversation context**: Sub-agents start fresh with no parent history
3. **Max depth**: 3 levels of delegation depth
4. **Same session events**: All events flow to the same session (tagged for identification)
5. **Plan folder restriction**: Sub-agents with `PlanFolderKey` set can only write to their plan folder
6. **Planner is research-only**: The planner sub-agent uses tools to research (read files, query data) but does NOT execute plan steps
