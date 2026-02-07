# Sub-Agent Delegation System

## Overview

Sub-agent delegation allows the main agent to spawn independent sub-agents to handle tasks. Two modes are supported:

1. **Simple delegation** (`delegate` tool) — Quick one-off tasks
2. **Plan-driven delegation** (`create_delegation_plan` + `execute_plan_task`) — Complex multi-step projects with task tracking

When delegation mode is enabled, the agent receives delegation tools and can spawn sub-agents with the same tool access as the parent (workspace, browser, skills, MCP servers).

---

## Quick Start

### Enabling Delegation Mode

Delegation mode is **enabled by default** for new chat sessions.

| Command | Action |
|---------|--------|
| `/spawn` | Enable sub-agent delegation |
| `/nospawn` | Disable sub-agent delegation |

### UI Indicator

When delegation mode is enabled, a small purple **GitBranch icon** appears next to the send button in the chat input.

---

## Delegation Tools

### 1. `delegate` — Simple One-Off Tasks

Spawns a sub-agent with a single instruction. Best for quick, independent tasks.

```json
{
  "name": "delegate",
  "parameters": {
    "instruction": "Clear, detailed instructions for the sub-agent",
    "reasoning_level": "high | medium | low (optional)"
  }
}
```

### 2. `create_delegation_plan` — Plan-Driven Delegation

Creates a structured plan with multiple tasks. Plan files (`.json` + `.md`) are saved to `Chats/Delegations/{planID}/`.

```json
{
  "name": "create_delegation_plan",
  "parameters": {
    "title": "Plan title",
    "tasks": [
      { "id": "task-1", "title": "...", "description": "...", "reasoning_level": "high" },
      { "id": "task-2", "title": "...", "description": "...", "reasoning_level": "medium" }
    ]
  }
}
```

### 3. `execute_plan_task` — Execute a Plan Task

Spawns a sub-agent for a specific task from a plan. The sub-agent is:
- **Instructed** to save all output to `Chats/Delegations/{planID}/`
- **Restricted** via FolderGuard to only write to that plan folder
- **Isolated** per-user via `X-User-ID` header (Chats/ routes to `_users/{userID}/Chats/`)

```json
{
  "name": "execute_plan_task",
  "parameters": {
    "plan_id": "plan-xxx",
    "task_id": "task-1",
    "reasoning_level": "high | medium | low (optional)",
    "additional_context": "Extra context for this execution (optional)"
  }
}
```

### 4. `update_plan_task` / `get_plan_status` — Plan Management

Update task status/notes or check overall plan progress.

---

## Multi-LLM Reasoning Tiers

Sub-agents can use different LLM providers/models based on task complexity:

| Tier | Use Case | Default |
|------|----------|---------|
| **high** | Complex reasoning, architecture decisions | Parent model (fallback) |
| **medium** | Standard coding, implementation | Parent model (fallback) |
| **low** | Simple tasks, formatting, lookups | Parent model (fallback) |

### Tier Configuration

**Priority order**: Frontend config > Environment variables > Parent model (fallback)

**Frontend**: Set via `delegationTierConfig` in chat request:
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

## Sub-Agent Capabilities

### Inherited from Parent

- **Workspace tools**: Read/write workspace files (with FolderGuard restrictions)
- **Browser tools**: If enabled in parent session
- **MCP servers**: Same server connections as parent
- **Skills**: Selected skills are passed to sub-agent system prompt via `buildSkillPrompt()`

### FolderGuard Restrictions

| Mode | Allowed Write Folders |
|------|----------------------|
| **Simple delegate** | `Chats/` (full folder) |
| **Plan task** | `Chats/Delegations/{planID}/` only |
| **With skill-creator** | Adds `skills/custom/` to allowed list |

All modes block `_users/` directory access. Per-user isolation is handled by the workspace API via `X-User-ID` header.

### Isolation

- **No parent context**: Sub-agents start fresh with no access to parent conversation
- **No sub-delegation**: Sub-agents cannot spawn their own sub-agents (prevents runaway chains)
- **Max depth**: 3 levels of delegation depth
- **Same session**: All events flow to the same session ID (tagged with delegation metadata)

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

When plans are created/updated, a `workspace_file_operation` event is emitted via `PlanEventEmitter`. This triggers the workspace sidebar to highlight the plan file.

---

## Frontend UI

### Delegation Start Event
- Shows `🔀` emoji with instruction summary (truncated to 80 chars)
- **+/−** expand indicator to toggle full details
- **Reasoning level badge**: Colored by tier (red=high, yellow=medium, green=low)
- **Live stats**: Token count and tool call count updated in real-time from child events
- Expanded view shows: full instruction, reasoning level, model ID, depth, delegation ID

### Delegation End Event
- Shows `✅` (success) or `❌` (failure) with summary
- **+/−** expand indicator for full details
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
| **Delegation Tools** | `agent_go/cmd/server/virtual-tools/delegation_tools.go` | Tool definitions, plan CRUD, `PlanEventEmitter` interface |
| **Server Integration** | `agent_go/cmd/server/server.go` | `executeDelegatedTask()`, folder guards, event emission |
| **Event Observer** | `agent_go/internal/events/event_observer.go` | `DelegationEventObserver` — tags sub-agent events |
| **Event Store** | `agent_go/internal/events/event_store.go` | `DelegationStartEventData`, `DelegationEndEventData` structs |
| **Agent Metrics** | `agent_go/pkg/agentwrapper/llm_agent.go` | `GetMetricsSnapshot()` for post-invoke token/tool stats |
| **Frontend Events** | `frontend/src/components/events/EventDispatcher.tsx` | Delegation event rendering with expand/collapse |
| **Event Hierarchy** | `frontend/src/components/events/EventHierarchy.tsx` | Live `delegationStats` computation from child events |
| **Frontend Toggle** | `frontend/src/components/ChatInput.tsx` | `/spawn` and `/nospawn` handlers |
| **Store** | `frontend/src/stores/useAppStore.ts` | `enableDelegationMode` state |

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
| `PlanFolderKey` | `string` | Plan-specific output folder (e.g., `Chats/Delegations/{planID}`) |

---

## Plan File Structure

Plans are stored as dual files in `Chats/Delegations/{planID}/`:

- `plan.json` — Machine-readable plan data
- `plan.md` — Human-readable markdown plan (visible in workspace sidebar)

The workspace API routes `Chats/` to `_users/{userID}/Chats/` via per-user isolation.

---

## Limitations

1. **No sub-agent delegation**: Sub-agents cannot spawn their own sub-agents
2. **No conversation context**: Sub-agents start fresh with no parent history
3. **Max depth**: 3 levels of delegation depth
4. **Same session events**: All events flow to the same session (tagged for identification)
5. **Plan folder restriction**: Plan task sub-agents can only write to their plan folder
