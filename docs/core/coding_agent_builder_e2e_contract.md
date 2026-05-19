# Coding Agent Builder E2E Contract

This file defines the builder-layer contract for coding-agent integrations in
`mcp-agent-builder-go`.

The low-level provider/tmux contract lives in:

```text
/Users/mipl/ai-work/multi-llm-provider-go/docs/coding_sdk_tmux_contract.md
```

That provider contract owns CLI launch details, prompt paste, ready/completion
detection, final text extraction, cancellation, and provider-specific real CLI
tests. This builder contract owns how those provider guarantees are used by the
HTTP API, workflow runtime, event store, MCP bridge, and frontend.

## Scope

The builder must prove these user-facing flows end to end:

- Workflow builder chat.
- Normal chat with a coding provider.
- Workflow step execution.
- Todo-task orchestrators and parallel sub-agents.
- Background agents.
- Live steering while an agent is still running.
- Cancellation from API/client disconnect and explicit stop.
- Terminal center viewing, selection, status, and scroll behavior.
- Chat history resume and event replay.

## Runtime Invariants

1. Chat and workflow shell working directory must match the caller workspace.
   Coding agents and `execute_shell_command` must see the same cwd.
2. Every coding-agent call that uses MCP must receive the correct per-session
   MCP bridge URL/token and must not accidentally use a different session.
3. Workflow steps, sub-agents, and background agents default to bounded tmux
   lifecycle. Chat defaults to persistent tmux lifecycle.
4. Bounded tmux terminals must remain viewable for the configured retention
   window, expose `closes_at`, then be cleaned up.
5. Completed terminal snapshots must remain selectable in the UI. Active
   terminal refresh must not steal selection from a manually selected terminal.
6. Terminal refresh must not reset scroll when the user has scrolled away from
   the bottom.
7. Terminal debug IDs must be copyable from the UI without cluttering the normal
   visible terminal view.
8. Unified completion must not duplicate terminal output, tool panels, or stale
   streaming text.

## Required Builder E2E Matrix

| Area | Required proof |
| --- | --- |
| Chat launch | Start a chat with a coding provider, verify provider/model label, cwd, MCP bridge, and first assistant response. |
| Multi-turn memory | Turn 1 asks the provider to remember a random canary without writing it to disk. Turn 2 asks for it. The second submitted prompt must not contain the canary. |
| Literal prompt text | Send a real chat turn containing a literal social handle such as `@fixyo.urflow`. The provider must treat it as text, not an `@path`, and return the handle without opening a debug console or hanging. |
| Live steer | Send a second user message while the agent is running. It must be routed to the same coding-agent session or provider queue, not create a duplicate run. |
| Cancellation | Stop/cancel must interrupt the coding provider and produce a canceled event, not a false completed response. |
| Workflow step | Run a code-exec workflow step and verify the coding agent starts in the workflow execution directory and writes the expected output file. |
| Query step | `query_step` by step id must resolve the active/latest execution id and return live progress for running steps plus stored logs for completed steps. |
| Todo orchestrator | Run a todo-task that launches parallel sub-agents. Each child must have distinct execution id, terminal id, MCP session, and output directory. |
| Background agent | Start a background agent and verify terminal/event visibility while it runs and completion notification when done. |
| Terminal center | Active, completed, failed, and closing terminals must show correct state, title/meta, debug copy, and selected terminal content. |
| Terminal scroll | While terminal content refreshes, manually scrolling up must preserve scroll position. If already at bottom, output should auto-follow. |
| Bounded retention | Completed step/sub-agent/background terminals must show `state=closing` and `closes_at`, remain viewable during retention, then disappear/close after cleanup. |
| Resume/history | Refresh or reopen the chat and verify events, completion, and terminal records restore consistently without mixing sessions. |
| UI formatting | Markdown/unified completion and terminal text must render in their separate surfaces without table/list collapse or terminal text leakage. |

## Provider Coverage

Builder E2E should run against every enabled coding provider that the selected
environment supports. At minimum:

- Claude Code tmux transport.
- Codex CLI tmux transport.
- Gemini CLI tmux transport.
- Cursor CLI tmux transport when installed.
- OpenCode structured JSON transport when enabled.

Provider-specific certification remains in the provider contract. Builder E2E
only needs to verify that the builder passes the correct runtime context and
handles the provider result/events correctly.

## Test Data Requirements

Real E2E tests should use:

- A temporary workflow under `workspace-docs/Workflow/...`.
- A deterministic random canary per test run.
- A small output file in the step output directory.
- Low/cheap model tiers unless the test specifically certifies a higher tier.
- Real MCP bridge calls, not fake tool output, for at least the workflow-step and
  todo-orchestrator paths.

Fake/unit tests are allowed for renderer, parser, and event-store edge cases,
but they do not certify the coding-agent integration.

## Related Files

- `agent_go/cmd/testing/coding_agent_chat_e2e.go`
- `agent_go/cmd/server/coding_agent_modes_test.go`
- `agent_go/internal/terminals/store_test.go`
- `frontend/src/components/TerminalCenter.tsx`
- `frontend/src/components/EventDisplay.tsx`
- `docs/core/mcp_bridge_layer.md`
- `docs/workflow/workflow_shell_working_directory.md`
