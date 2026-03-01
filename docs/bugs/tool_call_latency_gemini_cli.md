# Bug Report: Tool Call Start Latency in Gemini CLI Mode

## Status: Under Investigation 🔍

## Symptoms
- Noticeable delay between the LLM deciding to call a tool and the tool actually executing.
- Frontend receives `tool_call_start` event promptly, but actual tool execution starts ~1s later.
- The `_Staging_Bank_Manish` sheet (and similar data-dependent sheets) not loading expected query results — traced to static/hardcoded values in the sheet rather than live formulas, but latency investigation surfaced the broader timing issue.
- One extreme case observed: `Duration: 1m16.597159041s` for a single LLM response (Gemini CLI terminated with `signal: terminated` mid-execution).

## Environment
- Provider: Gemini CLI (subprocess mode)
- Tool execution path: Gemini CLI subprocess → HTTP POST to `$MCP_API_URL/tools/custom/{tool}` → server `/api/execute`
- Log file: `agent_go/logs/server_debug.log`

## Observed Latency Breakdown (from logs 2026-03-01)

```
⏱️  LLM REQUEST START       12:58:34   —
⏱️  LLM RESPONSE RECEIVED   12:58:46   +12s  (LLM thinking/streaming)
    tool_use event decoded   ~12:58:46  ~0ms  (Gemini CLI stdout)
    tool_call_start → frontend           0ms  (immediate, correct)
⏱️  TOOL EXECUTION START     12:58:46   +~1s  (HTTP roundtrip: Gemini CLI → server)
⏱️  TOOL EXECUTION END       12:58:48   +2.1s (actual tool runtime)
```

The ~1s gap between the frontend seeing `tool_call_start` and actual execution starting is the HTTP roundtrip from the Gemini CLI subprocess back to the server's `/api/execute` endpoint.

## Root Cause (Suspected)

In Gemini CLI mode, tool execution is a **two-hop process**:

1. Gemini CLI subprocess announces the tool call via stdout (`tool_use` event)
2. Server reads stdout → emits `ToolCallStart` → frontend notified ✅
3. Gemini CLI subprocess separately makes an HTTP POST to `$MCP_API_URL/tools/custom/{tool}` to actually run the tool
4. Server executes tool → returns HTTP response to Gemini CLI
5. Gemini CLI sends `tool_result` on stdout → server emits `ToolCallEnd` → frontend notified

The gap in step 3 is caused by:
- Gemini CLI parsing its own decision (step 1→3 internal delay)
- localhost HTTP overhead (small but non-zero)
- Server request routing and session lookup before execution

This is fundamentally different from the native `conversation.go` path where tool dispatch is in-process with no HTTP hop.

## Key Code Locations

| Component | File | What Happens |
|---|---|---|
| Gemini CLI stream parsing | `multi-llm-provider-go/pkg/adapters/geminicli/geminicli_adapter.go:471` | Converts `tool_use` stdout → `StreamChunkTypeToolCallStart` |
| ToolCallStart event emission | `mcpagent/agent/llm_generation.go:307` | Emits `ToolCallStartEvent` to frontend |
| Event bridge to frontend | `agent_go/cmd/server/event_bridge/base_bridge.go:176` | `EventStore.AddEvent` → polling API |
| Custom tool execution (Gemini CLI path) | `mcpagent/executor/handlers.go:348` | Actual tool runs here, after HTTP hop |
| MCP tool execution | `mcpagent/executor/handlers.go:251` | For MCP server tools |
| Native tool execution (non-CLI path) | `mcpagent/agent/conversation.go:1270` | In-process, no HTTP hop |

## Timing Logs Added (2026-03-01)

Added `⏱️` prefix logs to measure each segment:

- `mcpagent/agent/conversation.go` — `TOOL DISPATCH START`, `TOOL EXECUTION START`, `TOOL EXECUTION END` (for native/non-CLI path)
- `mcpagent/executor/handlers.go` — `TOOL EXECUTION START`, `TOOL EXECUTION END` (for Gemini CLI custom tool path and MCP tool path)

Filter in logs:
```bash
grep "⏱️.*TOOL\|⏱️.*LLM" agent_go/logs/server_debug.log
```

## Questions to Investigate

1. What is the consistent p50/p95 latency for the Gemini CLI HTTP hop (step 3→4 above)?
2. Is the `get_multiple_spreadsheet_summary` Google Sheets tool consistently slow (~10-12s)? Could be rate limiting or large response size.
3. Why does Gemini CLI sometimes take significantly longer before sending `tool_use` on stdout vs when the LLM response is fully received? Is there buffering in the CLI?
4. Can we reduce the HTTP roundtrip by having the server intercept `tool_use` events and execute tools directly (bypassing Gemini CLI's own HTTP call)?

## Related
- `_Staging_Bank_Manish` sheet: populated with static hardcoded values (Excel serial number dates), not live formulas. The Withdrawals sheet formula `IF(is_business, "_Staging_Bank_Manish", "")` correctly routes business transactions there, but the sheet itself only gets updated when bank records are manually pasted. See spreadsheet `1KiH3N8RNft-xRh_rxyZHBxuPlFMwytiJSBPE0ALFrwU`.
</content>
</invoke>