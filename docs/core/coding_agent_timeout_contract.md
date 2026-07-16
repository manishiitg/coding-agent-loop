# Coding-Agent Timeout Contract

Coding-agent execution has several independent timeout layers. They must not
be treated as one generic "agent timeout": each layer has a different owner and
recovery action.

## Effective runtime matrix

| Layer | Claude Code | Codex CLI | Cursor CLI | Pi CLI | Owner |
|---|---:|---:|---:|---:|---|
| Provider turn deadline | none | none | none | none | Workflow/scheduler caller |
| Provider MCP-call control | `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT` = 90m | `mcp_servers.api-bridge.tool_timeout_sec` = 90m | no documented control | no documented control | CLI provider |
| `mcpbridge` HTTP timeout | 5m ordinary, 90m long-running | same | same | same | `mcpbridge` |
| Server tool timeout | 90m in desktop runtime | same | same | same | `TOOL_EXECUTION_TIMEOUT` |
| Persistent tmux idle retention | 3h | 3h | 3h | 3h | Provider adapter |
| Initial prompt readiness | 5m inactivity by default | 10m inactivity, 90m absolute | 5m | 5m | Provider adapter |

The shared 90-minute MCP value is configured by
`CODING_AGENT_MCP_TOOL_TIMEOUT`. Invalid, zero, or negative values fall back to
90 minutes so the final stuck-call guard cannot be accidentally disabled.

## Required behavior

1. Workflow and scheduler contexts own the business deadline. Provider adapters
   do not impose a shorter fixed turn deadline on an active tmux process.
2. `call_sub_agent` and `call_generic_agent` are durable asynchronous starts.
   They return an `execution_id` immediately, so they do not hold an MCP request
   open for the child runtime.
3. HTTP request cancellation is authoritative. Stopping a parent cancels an
   in-flight bridge/custom/virtual request; handlers must not replace the
   request context with `context.Background()`.
4. The 90-minute MCP value is only a final backstop for a silent or stuck call.
   It is not a normal completion signal and is not extended by tmux pane text.
5. Timeout and cancellation errors identify the firing layer, tool, session,
   and configured timeout so operators can distinguish a bridge timeout from a
   workflow or prompt-readiness timeout.

## Provider notes

- **Claude Code:** the supported client watchdog is supplied in milliseconds.
- **Codex CLI:** the bridge server's `tool_timeout_sec` is supplied in seconds.
- **Cursor CLI:** current official CLI configuration documents MCP discovery and
  management but no MCP-call timeout setting. Do not invent an undocumented
  flag; rely on cancellation and `mcpbridge`.
- **Pi CLI:** the current Pi settings and MCP adapter expose no MCP-call timeout
  setting. Do not invent an undocumented setting; rely on cancellation and
  `mcpbridge`.

## Verification

Fast contract tests cover:

- the shared default and override policy;
- Claude millisecond and Codex second conversion;
- cancellation propagation through custom and virtual per-tool HTTP routes;
- deterministic timeout cleanup for a silent custom tool;
- bridge timeout/cancellation diagnostics.

The real tmux path is exercised by `mcp-agent test coding-agent-chat-e2e`. It
uses a temporary chat workspace, calls `execute_shell_command` through the
actual `api-bridge`, waits through a silent `sleep`, sends live input during an
active tool call, and stops the session during cleanup. Run it once per active
provider when provider credentials are available.
