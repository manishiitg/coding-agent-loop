# Browser Session Identity Split Plan

## Problem

Today a single MCP session ID is doing two different jobs:

1. **Tool session identity**
   - session-scoped `MCP_API_URL`
   - custom tool routing
   - code execution mode HTTP calls
   - session registry lookup for tool execution

2. **Browser session identity**
   - Playwright / `agent_browser` / Camofox browser reuse
   - page state continuity
   - login/session persistence across steps and follow-up inspection

This coupling works for simple cases, but it breaks down in workflow builder.

## Concrete Failure

### Builder + `run_saved_main_py`

Current workshop behavior:

- `run_saved_main_py(step_id, group_id)` executes through the workshop controller
- the controller switches into a **group MCP session** like:
  - `session-group-<group-id>-...`
- that session owns the live browser state opened by the saved `main.py`

But the main workflow-builder chat agent is different:

- it is the server-created chat `underlyingAgent`
- workshop tools are registered onto that agent
- its own MCP/tool calls continue using the **chat session**

Result:

- `run_saved_main_py` opens the browser in the group session
- the next builder message still uses the chat session
- the builder often cannot inspect the same live browser state
- if the old group session is gone from the session registry, the system creates a new browser

This is the root design issue:

- the builder wants to keep its own tool-routing context
- but it also wants to inspect the browser opened by a workflow step

## Current `share_browser` Behavior

`share_browser` already exists, but only as a **runtime delegation flag**, not as a general session model.

It is available on:

- `call_sub_agent(...)`
- `call_generic_agent(...)`
- generic multi-agent `delegate(...)`

Behavior today:

- `share_browser=true` or omitted
  - child keeps using the parent MCP session
  - browser state is shared
  - tool routing is also shared because both use the same MCP session ID

- `share_browser=false`
  - runtime generates an isolated MCP session ID
  - child agent config overrides `MCPSessionID` to that isolated value
  - browser state becomes isolated

Important:

- browser sharing/isolation is currently implemented by changing the whole `MCPSessionID`
- not by a separate browser-only identity

So `share_browser` confirms the same architectural coupling:

- browser identity and tool identity are the same thing today

## Goal

Separate the two identities.

### New Model

Each agent/session should be able to have:

- `tool_session_id`
  - used for MCP/custom tool routing
  - drives `MCP_API_URL`
  - should remain stable per chat/workflow agent

- `browser_session_id`
  - used for browser reuse only
  - drives Playwright / `agent_browser` / Camofox state
  - can be shared across agents when desired

## Proposed Browser Session Key

For workflow builder/workshop, browser identity should be stable per:

- workflow
- group

Recommended key shape:

- `browser::<workspace-hash>::<group-id>`

Alternatives like raw workflow name are easier to read, but more collision-prone.

Notes:

- use canonical `group_id`, not display name
- browser mode may optionally be appended if needed later

## Desired Behavior After Split

### Workflow builder

- builder chat agent keeps its own `tool_session_id`
- `run_saved_main_py` or `execute_step(group_id=...)` publishes the active `browser_session_id`
- subsequent builder browser inspection uses that `browser_session_id`
- builder shell/code-exec tools still stay on the chat/tool session

### Todo sub-agents

- default can remain shared browser when `share_browser=true`
- `share_browser=false` should only create a new **browser session**
- it should not require rebinding all tool routing

### Multi-agent chat

- default behavior should remain isolated unless explicitly opting into shared browser state
- multiple agents may share a browser only when that is intentional
- generic multi-agent behavior should not silently start sharing browser state globally

## What Must Change

### 1. Session model

Current:

- one `MCPSessionID`

Target:

- `ToolSessionID`
- `BrowserSessionID`

This should be introduced in a backward-compatible way:

- if `BrowserSessionID` is empty, fall back to `ToolSessionID`

### 2. Browser-capable tools

All browser access paths must use `browser_session_id` instead of assuming the tool session:

- Playwright MCP
- `agent_browser`
- Camofox MCP

These tools should:

- resolve `browser_session_id` first
- fall back to `tool_session_id` if no browser session override is present

### 3. Code execution env

Keep:

- `MCP_API_URL` based on `tool_session_id`

Add:

- browser-session propagation separately for browser tools

This may be via:

- explicit env like `MCP_BROWSER_SESSION_ID`
- explicit request metadata/context
- executor-level session override

The exact mechanism can be decided during implementation, but it must avoid rebinding the whole tool session just to inspect a browser.

### 4. Workflow/workshop controller state

Workshop/session state should track:

- active browser session per group
- last active browser session for the current builder context

Likely state:

- `map[groupID]browserSessionID`
- optional `lastActiveGroupID`

### 5. Cleanup lifecycle

Browser cleanup must be separated from tool session cleanup.

Today:

- closing MCP session often closes the browser tied to that session

After split:

- closing a tool session must not automatically destroy a shared browser session unless explicitly requested
- browser lifecycle should be managed independently

## Suggested Rollout

### Phase 1: Internal split with compatibility fallback

Introduce:

- `tool_session_id`
- `browser_session_id`

Keep default behavior:

- browser session falls back to tool session

This avoids breaking existing code immediately.

### Phase 2: Workflow builder/workshop adoption

Use the split only in workflow builder/workshop first:

- `run_saved_main_py`
- `execute_step`
- follow-up builder browser inspection

This is the main user-facing fix we want.

### Phase 3: Delegation/tooling adoption

Change:

- `share_browser=false` to isolate browser session only
- not the full tool session unless truly needed

### Phase 4: General multi-agent adoption

Apply to generic multi-agent chat only if needed, and keep shared browser opt-in.

## Files Likely Involved

### In `mcp-agent-builder-go`

- `agent_go/pkg/orchestrator/base_orchestrator.go`
  - current single-session propagation and env updates
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go`
  - workshop group session cache
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_workshop.go`
  - workshop group switching and step execution
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`
  - agent config overrides, current `share_browser` handling
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_exports.go`
  - workshop session setup and current MCP/session propagation rules
- `agent_go/cmd/server/server.go`
  - workflow-phase chat agent creation, workshop tool registration, session-aware executors

### In `mcpagent`

- browser session registry / reuse logic
- Playwright session lookup
- `agent_browser` execution path
- Camofox session lookup
- any code assuming tool session ID == browser session ID

## Expected Benefits

- builder can inspect the same live browser opened by `run_saved_main_py`
- workflow code execution keeps stable tool routing
- browser sharing becomes explicit and controllable
- `share_browser` becomes semantically correct
- browser reuse logic becomes consistent across:
  - Playwright
  - `agent_browser`
  - Camofox

## Main Risk

If browser sessions are shared too broadly:

- two agents may interfere with the same page/session
- parallel browsing may become nondeterministic

So shared browser reuse should be:

- explicit
- scoped
- default-on only where already expected, such as parent/sub-agent sharing in the same task

## Recommendation

Implement the split.

Short version:

- keep `tool_session_id` for tool routing
- introduce `browser_session_id` for browser reuse
- adopt it first in workflow builder/workshop
- make Playwright, `agent_browser`, and Camofox all honor the browser session layer
