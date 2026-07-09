# Sub-Agent Delegation System

## Overview

Sub-agent delegation lets the main agent spawn independent sub-agents to run work in parallel. It is available in **Multi Agent Chat** mode only — when the user selects the Multi Agent Chat mode category, the main agent automatically receives delegation tools and always runs in direct-delegate mode.

There is no planning phase, no `/spawn` toggle, no `plan_folder` scoping. Sub-agents always run in **code_execution** tool mode (Python harness calling MCP tools via HTTP API) and all delegation is **asynchronous** — sub-agents run in the background and auto-notify the manager when they complete via synthetic turns.

---

## Delegation Tools

The main agent in Multi Agent Chat is wired with these tools (defined in `agent_go/cmd/server/virtual-tools/delegation_tools.go`):

| Tool | Purpose |
|---|---|
| `delegate` | Spawn a sub-agent to run a task in the background. Returns immediately with an `agent_id`. |
| `query_agent` | Check status / recent conversation history of a running background agent. |
| `terminate_agent` | Cancel a running background agent. |
| `list_agents` | List all background agents in the current session with status and elapsed time. |
| `run_workflow` / `run_step` / `stop_workflow_run` | Start and stop existing workflow executions from Multi Agent Chat. |

### `delegate` — Execute a Task

Spawns a background sub-agent and returns immediately. The sub-agent runs async and the manager is notified on completion.

```json
{
  "name": "delegate",
  "parameters": {
    "name": "Short descriptive name shown to the user (e.g. 'Research APIs')",
    "instruction": "Comprehensive, self-contained instructions — workers have no shared memory",
    "reasoning_level": "high | medium | low | <custom-tier> (required in multi-agent mode)",
    "agent_template": "Optional sub-agent template folder name from subagents/",
    "servers": ["optional", "list", "of", "mcp", "servers"],
    "share_browser": true
  }
}
```

**Returns** (async):
```json
{
  "async": true,
  "agent_id": "delegation-0-1234567890",
  "name": "Research APIs",
  "status": "running",
  "message": "Background agent 'Research APIs' started. You'll be notified when it completes. Use query_agent(agent_id: \"...\") to check status."
}
```

**Required fields** (enforced by `handleDelegate` in `delegation_tools.go`):
- `name`, `instruction`, and `reasoning_level` are required in Multi Agent Chat mode.
- `reasoning_level` must be one of the configured tier names; invalid values are silently ignored and the parent model is used as a fallback.

### `query_agent`, `terminate_agent`, `list_agents`

Managed by a `BGAgentRegistry` in the server. `query_agent(agent_id, last, offset)` supports pagination over a background agent's conversation history so the manager can inspect in-progress work without blocking.

### `run_workflow`, `run_step`, `stop_workflow_run`

`run_workflow` and `run_step` start existing workflows in the background and return an `agent_id`. `stop_workflow_run(agent_id)` is the workflow-specific cancellation path; it cancels both the background-agent wrapper and the child workflow session/orchestrator context. Use `terminate_agent` for delegated sub-agents, not workflow runs.

---

## Execution Model

### Async, Always

In Multi Agent Chat the `BackgroundDelegateKey` context value is always set. When the manager calls `delegate()`, `handleDelegate` hits the async branch, spawns a background agent, and returns the `agent_id` immediately. The manager then **ends its turn**.

### Auto-Notification via Synthetic Turns

When a background sub-agent finishes:

1. `backgroundCompletionLoop` (`server.go:8637`) queues the completed agent ID via `queuePendingCompletion`.
2. A batching layer drains pending completions and either calls `processBackgroundAgentCompletion` (single) or `processBatchedBackgroundAgentCompletions` (multiple close together).
3. That handler builds an `[AUTO-NOTIFICATION]` message containing the agent name, status, and full result, then calls `executeSyntheticTurn(sessionID, syntheticMsg)` (`server.go:8806+`).
4. `executeSyntheticTurn` drives the stored session agent directly with the synthetic message — the manager runs a new turn without needing a user message.
5. `IsAutoNotification` / `setSyntheticTurn` / `isSyntheticTurn` tell the frontend to still accept user input during synthetic turns. If a real user message arrives mid-synthetic-turn, the synthetic turn is canceled and the user message takes priority (`server.go:3122+`).

The same synthetic-turn infrastructure is shared with the workshop/builder (see `server.go:4900-4901`) — only the `[AUTO-NOTIFICATION]` content differs.

---

## Sub-Agent Configuration

### Always Code Execution

Every sub-agent runs with `UseCodeExecutionMode: true`. This means the worker:
- Gets `get_api_spec` (virtual tool) + `execute_shell_command` (direct tool)
- MCP tools are accessed via HTTP API (`POST /tools/mcp/{server}/{tool}`) instead of as direct function calls
- Current custom tools (`workspace_advanced`, `human_tools`, and other registered built-ins) remain as direct tools
- `MCP_API_URL` and `MCP_API_TOKEN` env vars are available in the shell environment

### Inherited from Parent

Sub-agents inherit most configuration from the parent request:

| Setting | Source | Fallback |
|---|---|---|
| Temperature | Parent request | 0.7 |
| MaxTurns | Parent request | 100 |
| ToolTimeout | `TOOL_EXECUTION_TIMEOUT` env var | 5 minutes |
| Summarization | All parent summarization fields | Parent defaults |
| Context Editing | All parent context-editing fields | Parent defaults |
| LargeOutputThreshold | `LARGE_OUTPUT_THRESHOLD` env var | Default |
| MCP servers | Parent's enabled servers (or the `servers` override from the delegate call) | — |
| Browser session | Shared with parent unless `share_browser: false` | — |

### Reasoning Tiers

`reasoning_level` selects a provider/model from the delegation tier config:

| Tier | Use Case |
|---|---|
| `high` | Complex reasoning, architecture decisions |
| `medium` | Standard coding, implementation |
| `low` | Simple tasks, formatting, lookups |
| `<custom>` | User-defined tiers in the tier config |

**Priority order for tier config**: Frontend request (`delegation_tier_config`) > environment variables (`DELEGATION_HIGH_PROVIDER`, `DELEGATION_HIGH_MODEL`, etc.) > parent model (fallback).

Frontend configuration lives in the tier-config chip in the chat input and the **Delegation Models** section in the left sidebar. It's sent as `delegation_tier_config` in the chat request.

### Folder Guard

All sub-agents use the default Chats/ folder guard:
- **Writable**: `Chats/`, `Downloads/`, `config/`, `memories/`, plus `skills/custom/` and `subagents/custom/` when the corresponding builder tool is active.
- **Readable**: `Chats/`, `Downloads/`, `skills/`, `subagents/`, `Workflow/`, `config/`, `memories/`.
- `_users/` is blocked. Per-user isolation is handled at the workspace API layer via the `X-User-ID` header.

There is no longer a tighter plan-folder-specific guard — the plan-driven execution path has been removed entirely.

### Isolation

- **No parent context**: Sub-agents start fresh with no access to parent conversation history.
- **No sub-delegation**: Sub-agents cannot spawn further sub-agents.
- **Max delegation depth**: 3 (enforced by `MaxDelegationDepth` in `delegation_tools.go`).
- **Same session**: All events flow to the parent session, tagged with delegation metadata (`component: "delegation-{depth}"`, `correlation_id: "delegation-{index}-{timestamp}"`, `parent_id` linking to the `delegation_start` event).

---

## Sub-Agent Templates

Users can define reusable sub-agent profiles under `subagents/<name>/SUBAGENT.md` with YAML frontmatter:

```markdown
---
name: code-review
description: Specialized code review agent
default_reasoning_level: high
skills: code-review, security-scan
servers: github
---

# Instructions
You are a code review specialist...
```

Supported frontmatter fields:
- `name` (required)
- `description` (required)
- `default_reasoning_level` (optional)
- `skills` (optional)
- `servers` (optional)

When the manager calls `delegate(..., agent_template: "code-review")`, the backend loads the template and applies its instructions, default reasoning level, and auto-activates the configured skills and MCP servers for the sub-agent. Templates are listed to the manager via `BuildSpawnCapabilitiesSection` so it knows what's available.

---

## Event System

### Delegation Events

**`delegation_start`** — emitted when a sub-agent is spawned:
```json
{
  "type": "delegation_start",
  "data": {
    "delegation_id": "delegation-0-1234567890",
    "depth": 0,
    "instruction": "Task instruction...",
    "reasoning_level": "high",
    "model_id": "claude-sonnet-4-6",
    "servers": ["github", "brave-search"],
    "background_agent_id": "...",
    "agent_template": "code-review"
  }
}
```

**`delegation_end`** — emitted when a sub-agent completes:
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

Events from sub-agents are tagged by `DelegationEventObserver` (`agent_go/internal/events/event_observer.go`):

```json
{
  "component": "delegation-0",
  "hierarchy_level": 1,
  "correlation_id": "delegation-0-1234567890",
  "parent_id": "session_delegation_start_delegation-0-1234567890"
}
```

---

## Context Keys

Defined in `delegation_tools.go`:

| Key | Type | Purpose |
|---|---|---|
| `ExecuteDelegatedTaskKey` | `ExecuteDelegatedTaskFunc` | Sync path: function to spawn a blocking sub-agent (used outside multi-agent mode) |
| `BackgroundDelegateKey` | `BackgroundDelegateFunc` | Async path: function to spawn a background sub-agent (always set in multi-agent chat) |
| `DelegationDepthKey` | `int` | Current delegation depth (capped by `MaxDelegationDepth`) |
| `WorkspaceClientKey` | `*workspace.Client` | Workspace client for file I/O |
| `DelegationTierConfigKey` | `*DelegationTierConfig` | Multi-LLM tier configuration |
| `ReasoningLevelKey` | `string` | Reasoning level selected for the current delegation |
| `CapabilitiesContextKey` | `*CapabilitiesContext` | Available MCP servers, skills, and sub-agent templates |
| `AgentTemplateKey` | `string` | Sub-agent template folder name (from `agent_template` param) |
| `DelegationServersKey` | `[]string` | MCP servers scoped to this sub-agent (from `servers` param) |
| `ShareBrowserKey` | `bool` | False when the delegate call asked for browser isolation |
| `SessionEventEmitterKey` | `SessionEventEmitter` | Emits blocking human-feedback / question events for input UIs |
| `BGAgentRegistryKey` | `BGAgentQuerier` | Registry used by `query_agent` / `list_agents` / `terminate_agent` |
| `BGAgentSessionIDKey` | `string` | Session ID for the background agent registry |
| `BackgroundAgentIDKey` | `string` | Links a background agent to its parent delegation |
| `ToolEventCallbackKey` | `ToolEventCallback` | Tool call timing callback for background agents |

---

## System Prompt

The main agent's system prompt is built from `GetMultiAgentDelegationInstructions()` in `delegation_tools.go`. Key rules delivered to the manager:

- Default to breaking tasks into sub-tasks and delegating directly — no planning phase.
- Call `delegate()` multiple times in one turn for parallel execution.
- Always pass `reasoning_level` and self-contained `instruction`.
- End the turn after delegating; auto-notification will wake the manager when work completes.
- Review results before reporting back; the manager is the quality gate.
- For file outputs, create a descriptive sub-folder under `Chats/` and tell workers where to save.
- Never mention internal concepts ("sub-agents", "delegation", "synthetic turns", tool names) to the user.

Sub-agent templates (if any) are appended via `BuildSpawnCapabilitiesSection()`, and custom reasoning tiers via `BuildCustomTierPromptSection()`.

---

## Frontend UI

### Mode

Multi Agent Chat is selected from the mode switcher. There is no plan-phase selector, no `/spawn` toggle, no plan-folder picker in the sidebar — those have all been removed.

### Delegation Events in Chat

`EventDispatcher.tsx` renders `delegation_start` events with:
- Instruction summary (truncated to 80 chars)
- Expand indicator (+/−) for full details
- Reasoning level badge (color-coded: red=high, yellow=medium, green=low)
- Live stats: tool call count, token count, elapsed time (updated from child events with matching `correlation_id` via `EventHierarchy.tsx` `delegationStats`)
- Code Execution mode icon (every sub-agent runs in code_execution now)
- Agent template badge (when set)

`delegation_end` events show success/failure, inline stats (total tokens, tool calls, duration), and expand to show full result text.

### Tier Config

The tier-config chip in the chat input and the **Delegation Models** section in the left sidebar let users configure which provider/model backs each reasoning tier. The config is sent as `delegation_tier_config` in the chat request.

---

## Key Files

| Component | File Path | Description |
|---|---|---|
| Delegation tools | `agent_go/cmd/server/virtual-tools/delegation_tools.go` | Tool schemas, `handleDelegate`, `handleQueryAgent`, `handleTerminateAgent`, `handleListAgents`, reasoning tier helpers, `GetMultiAgentDelegationInstructions` |
| Server integration | `agent_go/cmd/server/server.go` | `executeDelegatedTask`, `executeBackgroundDelegatedTask`, `buildCapabilitiesContext`, synthetic turn orchestration, folder guard wrapping |
| Background agent registry | `agent_go/cmd/server/background_agents.go` | `BGAgentRegistry`, `BGAgentInfo`, history tracking |
| Shell command | `agent_go/pkg/workspace/execute_shell_command.go` | Shell execution client with non-zero exit → tool error |
| Event observer | `agent_go/internal/events/event_observer.go` | `DelegationEventObserver` — tags sub-agent events |
| Event store | `agent_go/internal/events/event_store.go` | `DelegationStartEventData`, `DelegationEndEventData` structs |
| Agent metrics | `agent_go/pkg/agentwrapper/llm_agent.go` | `GetMetricsSnapshot()` for post-invoke token/tool stats |
| Frontend events | `frontend/src/components/events/EventDispatcher.tsx` | Delegation event rendering with expand/collapse |
| Event hierarchy | `frontend/src/components/events/EventHierarchy.tsx` | Live `delegationStats` map from child events |
| Mode store | `frontend/src/stores/useModeStore.ts` | `ModeCategory` type (`'chat' \| 'workflow' \| 'multi-agent' \| null`) |
| Workspace sidebar | `frontend/src/components/Workspace.tsx` | `Chats/` folder filtering for multi-agent mode |

---

## Limitations

1. **No sub-sub-agents**: Sub-agents cannot delegate further. Max depth 3 (enforced, currently set to 3 via `MaxDelegationDepth`).
2. **No conversation context**: Sub-agents start fresh — no parent history, no shared memory.
3. **Same session events**: All events flow to the parent session and are tagged for identification.
4. **Code execution only**: There is no way to spawn a worker in "simple" or "tool_search" mode anymore. Every sub-agent runs the Python harness.
5. **No plan folder scoping**: Sub-agents can write anywhere the default Chats/ folder guard allows. Scoping to a specific sub-folder is done via the worker's instruction, not via a context flag.
