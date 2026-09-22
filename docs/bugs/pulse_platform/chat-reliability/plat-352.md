[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-352 — Simplify chat render and restoration to one durable ordered log (design)

| Field | Value |
|---|---|
| Status | `design direction agreed; persistence scope and migration not implemented` |
| Priority | P2 architecture |
| Owner | unassigned — design review first |
| Reported | 2026-09-22 |
| Related | PLAT-324 (retain conversations across reloads), PLAT-351 (retained-turn settle), commit `df8254c1a` (cursor-only restore) |

## Problem

Chat restore is correct but structurally complex, and the complexity keeps
producing work (`df8254c1a` removed one raw-window fetch; the reconciliation
it fed remains). Three compounding choices drive it:

1. **Two sources of truth.** A volatile in-memory event window (transport)
   plus durable conversation JSON (record) describe the same transcript in
   different shapes, so every restore reconciles them:
   `combineTranscriptTraceEvents` merges ui_events + currentEvents +
   liveEvents with ordering rules, then `transferLiveTailInputIdentity`,
   `preserveUnconfirmedAcceptedLiveInputs`, and `monotonicConversation`
   patch up identity and ordering. Three inputs to one converter is where
   the bug class lives.
2. **Identity is positional.** The resume cursor is an index into a
   *volatile* window (`baseIndex + len - 1`), so restart/trim shifts
   meaning and dedup needs cross-source ID transfer instead of plain set
   membership.
3. **Reads multiplied per caller.** Preview, resume snapshot, paged
   history, raw window, cursor, SSE replay — each product surface
   assembled its own combination, which is why `hydrateTabEvents` has a
   happy path, a legacy fallback, and a fallback-to-history inside the
   fallback.

## Proposed design

Make the per-session event log the only truth, and make it durable on
append:

- Every event (user message, assistant chunk, tool call, completion,
  status) appends to a per-session log with a **monotonic sequence
  number** assigned at write time, flushed to disk on append (WAL-style;
  fsync can be batched per turn, not per chunk).
- **Stable IDs, not positions.** The client generates an idempotency key
  per user message; the server echoes it on the authoritative entry.
  Dedup is `seen.has(id)` — no identity transfer, no monotonic guards.
- **Render is a pure function of (log, cursor).** Fresh restore =
  `GET log[0..tip]`. Resume = `GET log[cursor..tip]` then SSE from
  `tip`. Pagination = range queries. Preview = `log[0..k]` projected.
  One endpoint shape, one converter, every surface.
- **Optimistic UI the standard way.** The client renders provisional
  entries keyed by client ID; authoritative echo replaces them. No
  "unconfirmed accepted live input" preservation pass — just key
  matching.

What this deletes: the trace combiner, the identity transfer, the
live-tail append, the resume snapshot as a separate artifact (it becomes
a cached range), the cursor-vs-window distinction, and the legacy
fallback branch — roughly half of `sessionRestore.ts` — while the
snapshot-merge machinery (`mergeChatConversationSnapshots`, overwrite
guards) shrinks to "append wins, sequence decides."

## Current durable-journal finding

The repository already contains part of this foundation: one global SQLite
journal at `structured-chat-events.sqlite`. `EventStore.AddEvent` appends
non-streaming structured events with a stable event ID and monotonic
per-session sequence, and hydrates a bounded tail after restart. This means
the remaining work is not "introduce SQLite"; it is to narrow and compact
that journal, make chat readers authoritative on it, and remove the duplicate
conversation/UI-event representations.

The current journal is global and unbounded. It receives events from direct
and product chats, Builder/Crew, scheduled and headless workflow runs,
workflow steps, message-sequence items, and sub-agents. The in-memory
1,500-event limit does not delete SQLite rows, and the current 512 MB threshold
only logs a warning.

Measurements on 2026-09-22 confirmed that this is already material:

- Local: about 270 MB database + WAL in roughly 25 hours, containing 29,139
  events across 30 sessions. Of those, 13,890 were `workflow_step` events.
- RTS: about 67 MB database + WAL in roughly one day, containing 7,742 events
  across 175 sessions.
- Large repeated snapshots, rather than row count alone, drive growth. Local
  `conversation_turn` payloads totaled 46.7 MB and reached 1.04 MB each;
  `llm_generation_end` totaled 36.6 MB and reached 1.21 MB each.
- Scheduled workflow sessions were the largest individual local consumers,
  with one session using about 52 MB.

## Agreed persistence boundary

The durable structured journal is for **interactive conversation history**,
not general execution telemetry.

Persist:

- Workflow Builder chats;
- Crew chats;
- Work/project chats;
- product chats and other direct user conversations;
- compact parent-chat summaries for child/sub-agent lifecycle transitions.

Do not persist to the chat journal:

- scheduled, queued, or manual workflow runs;
- headless/full workflow executions;
- individual workflow-step or message-sequence-item events;
- sub-agent internal streams;
- token/streaming chunks, terminal frames, repeated status lines, or system
  prompts.

Execution products keep using their existing run/execution logs. A parent chat
may retain a compact reference to an execution without copying its internal
event stream into the chat journal.

The server must register an explicit persistence class when it creates a
session, for example `interactive_chat`, `execution`, or `ephemeral`.
Persistence must not be inferred from session-ID prefixes. Only
`interactive_chat` sessions may append to the durable chat journal; unknown
sessions fail closed to non-durable delivery until classified.

Even retained chat events must be semantic and compact. Do not put a complete,
growing conversation snapshot into each `conversation_turn` or
`llm_generation_end` row. Large tool results, terminal captures, screenshots,
and files belong in separate artifacts; the event stores a bounded summary and
stable reference.

## Canonical meaningful chat events

"Meaningful" is an explicit schema decision, not a runtime guess based on an
event name. A durable chat event must satisfy all of these conditions:

1. it changes the conversation state visible to the user;
2. that state must still be visible after refresh or server restart;
3. it has a stable event ID, turn ID, and ordered sequence position; and
4. its stored payload is bounded.

The initial durable allowlist is:

| Canonical type | Durable meaning |
|---|---|
| `user_message` | A user message accepted by the server |
| `assistant_message` | The consolidated assistant text for a turn |
| `assistant_progress` | A useful consolidated progress update, not a token chunk |
| `tool_call_started` | Tool name plus bounded/sanitized arguments |
| `tool_call_completed` | Result summary plus an optional artifact reference |
| `tool_call_failed` | Bounded tool error summary |
| `input_requested` | The chat is durably waiting for user input |
| `background_agent_started` | Compact child-agent identity and purpose |
| `background_agent_completed` | Compact child-agent outcome/reference |
| `turn_completed` | Successful terminal state for a turn |
| `turn_failed` | Failed terminal state for a turn |
| `turn_cancelled` | User/system cancellation terminal state |

The following remain live-only or go to their existing execution/diagnostic
stores and must not enter durable chat history:

- individual streaming tokens/chunks;
- terminal lines, frames, and repeated status-line updates;
- heartbeats, polling events, and token counters;
- system prompts and raw provider traces;
- complete conversation snapshots;
- internal workflow-step/message-sequence events;
- repeated copies of tool arguments or results.

Providers and execution engines may continue producing their existing raw
events. A single **chat-event projector** maps them onto the canonical
allowlist or classifies them as live-only/ignored. For example, hundreds of
assistant chunks become one `assistant_message`; terminal updates remain live;
a tool start/result pair becomes two canonical tool events; workflow-step
details remain in the execution log. Frontend code must consume the canonical
contract and must not repeat provider-specific interpretation.

Every canonical row uses one versioned envelope:

```json
{
  "schema_version": 1,
  "id": "stable-event-id",
  "session_id": "chat-id",
  "turn_id": "turn-id",
  "sequence": 145,
  "type": "tool_call_completed",
  "timestamp": "2026-09-22T12:00:00Z",
  "data": {
    "tool_name": "exec",
    "summary": "Tests passed",
    "artifact_id": "artifact-42"
  }
}
```

Set a hard encoded-row limit (initial proposal: 64 KiB, with stricter limits
for individual text fields). Payloads above the limit are rejected from the
journal until the producer writes the large content to a private artifact and
emits a bounded summary/reference. The projector must never silently truncate
content without marking the event and retaining a way to inspect the complete
artifact.

Adding another durable event type requires an explicit schema change plus
tests covering projection, restart restore, ordering, deduplication, payload
bounds, and redaction. Unknown raw types fail closed to live-only delivery.

## Step and agent developer diagnostics

Removing workflow-step internals from the main chat journal must not make them
uninspectable. Their durable home remains the workflow/run execution artifacts,
including per-step conversation JSON, tool calls/results, errors, outputs, and
metrics. The normal product surface remains **Execution Logs**; the existing
child/step terminal rail remains available only when server runtime diagnostics
are explicitly enabled.

### Current developer-diagnostics mode

The existing rail is not normal product navigation. `ChatArea` mounts
`TerminalCenter` only when both conditions are true:

1. `GET /api/capabilities` reports `runtime_debug: true`, which is controlled
   by the server's explicit `AGENTWORKS_RUNTIME_DEBUG` opt-in (local launcher:
   `--enable-chat-terminal-debugs`); and
2. the active chat tab is switched from formatted conversation to Terminal /
   Live view.

When `runtime_debug` is false, the same Live-view control shows only
`MainAgentTerminal`; individual workflow-step/child terminals are not exposed.
Normal users inspect completed or running step records through Execution Logs.
This distinction must remain explicit so an internal terminal inventory is not
accidentally promoted into every product chat.

The proposed Crew-style diagnostics surface should remain capability-gated and
developer/admin-oriented initially. It must:

- scope every terminal, step, artifact, and JSON read to the active authorized
  session/workspace and never provide a cross-user "view all" mode;
- lazy-load only after the developer opens Diagnostics, so ordinary chat startup
  performs no terminal inventory or execution-log polling;
- offer an explicit **Diagnostics** entry rather than silently changing the
  normal **Open live view** behavior;
- preserve the formatted conversation as the default view and make returning to
  it immediate;
- read existing execution artifacts, terminal/SSE state, and the cost ledger
  without creating a new persistence stream;
- clearly label checkpointed JSON versus live terminal data when the two have
  not yet converged.

Add a Crew-style developer diagnostics view over those existing artifacts:

```text
┌ Agents / steps ───────┬ Selected step ────────────────────┐
│ Main agent      ●     │ Conversation | Tools | Terminal   │
│ Research step   ✓     │ Raw JSON | Costs | Artifacts      │
│ Writer step     ●     │                                   │
│ Reviewer        !     │ User instruction                  │
│   Sub-agent     ✓     │ Assistant progress                │
│                       │ Tool call and result               │
│                       │ Final response                     │
└───────────────────────┴───────────────────────────────────┘
```

The view should reuse the existing execution-log API, `ConversationViewer`,
step status/metrics, artifact loader, and terminal renderer rather than create
another persistence format. Its left rail groups the main agent, workflow
steps, Crew members, and sub-agents; the detail pane provides formatted
conversation, tool calls, terminal, raw JSON, cost/token metrics, artifacts,
errors, and per-step search.

Durable step JSON is checkpoint-oriented and can lag while an execution is
running. The diagnostics view may overlay the existing live terminal/SSE feed
for current activity, then converge on the execution JSON when the step
completes. Live transport data must not be written into the main chat journal
to support this view.

The user-facing separation is therefore:

- main chat SQLite: the user's conversation plus compact run/child summaries
  and links;
- workflow execution JSON/logs: complete step and agent diagnostics;
- terminal/SSE: live low-level inspection;
- cost ledger: authoritative usage and cost data.

PLAT-352 only needs to preserve the data contract and links required by this
diagnostics surface. Shipping or promoting the diagnostics UI beyond the
existing runtime-debug capability can land separately without blocking the
single-source chat migration.

## Retention and migration requirements

- Deleting a conversation deletes its journal rows and referenced private
  artifacts.
- Define an explicit retention/archive policy for old conversations, plus a
  total-size guard; a warning alone is insufficient.
- Checkpoint the WAL and provide deliberate compaction after bulk deletion.
- Archive or rotate the existing mixed-scope journal once during migration.
  Existing conversation JSON remains the restoration fallback while chat
  sessions are moved to the filtered journal.
- Keep the inspectable conversation JSON as a derived/export projection during
  migration, not an independent competing source of truth.

## Migration (not a flag day)

1. Add the explicit session persistence class and stop journaling execution,
   workflow-step, message-sequence, and internal sub-agent streams.
2. Implement the canonical chat-event projector and move large payloads to
   referenced artifacts.
3. Archive/rotate the existing mixed journal, then retain/checkpoint the new
   chat-only journal under an explicit size policy.
4. Move readers over one surface at a time (chat, then Work/Crew/product
   chats), each switching to range reads + stable-ID dedup.
5. Delete the converters and duplicate persisted `ui_events` last, once no
   caller passes a second source.

## Caveats

- It grew this way for real reasons: the event store predates durable
  history, tmux CLIs emit unstructured output that must be
  *interpreted* into events, and each product needed restore before a
  shared primitive existed. A log-first rewrite does not remove the
  provider-interpretation layer — it stops duplicating its output in
  two shapes.
- Bounding still matters: 1.3 MB sessions mean range queries must stay
  paged and fat frames must stay out of the hot path (the existing
  `include_ui_events` split already proves this). Terminal snapshots
  stay opaque blobs attached to the log, not log entries.
- Open question for design review: per-turn fsync batching vs
  per-append durability for the crash-during-turn case, and whether the
  log file replaces the conversation JSON on disk or sits beside it
  during migration.

## Verification

Design acceptance requires an approved compact schema + endpoint shape (`GET
log` ranges, sequence/ID semantics), explicit session persistence classes,
retention/deletion behavior, and an archive/rotation plan for the existing
mixed journal. Implementation verification must additionally prove that
workflow-step and scheduled-run events do not create journal rows, interactive
chat restore survives restart, large payloads are referenced rather than
copied, and conversation deletion removes its durable rows.

## Appendix: event-type audit (2026-09-22)

All 118 cataloged types were checked for backend producers (mcpagent,
agent_go, provider repo) and frontend consumers (outside tests/generated).
Method: const-name references plus raw wire-string grep; stale binaries
excluded. A dispatcher branch alone does not count as live — emission was
verified per type. Result: **6 dead consts, 10 dead handlers/renderers, 13
produced-then-dropped, 11 telemetry demotions; ~78 remain genuinely live,
of which ~12 families become durable canonical.**

Delete outright — defined, never produced or consumed anywhere:

- `step_execution_start`, `step_execution_end`, `step_execution_failed`
- `decision_evaluated`, `prerequisite_navigation`, `context_editing_started`

Delete the consts, generated TS types, and referencing tests.

Dead frontend handling — rendered or retained, nothing emits them:

- `live_execution_streaming` (full `EventDispatcher` renderer, dead)
- `cache_event` (renderer, dead)
- `phase_started`, `phase_completed` (retention-list entries only, dead)

Dead renderers — dispatcher branch exists, zero emitters (verified, not
just unreferenced):

- `mcp_server_connection`, `mcp_server_discovery`,
  `mcp_server_connection_error` (only the `_start` variant is emitted,
  which itself has no renderer)
- `model_change` (`NewModelChangeEvent` is never called)

Never displayed — emitted but hidden or nulled before the screen:

- `system_prompt` (in `HIDDEN_EVENTS`, never displayed anywhere)
- `mcp_server_selection` (dispatcher explicitly returns null: "internal
  per-turn routing decision, never rendered")

Produced then thrown away — `SKIP_EVENTS` in `base_bridge.go` already drops
these ("no UI component, pure waste"), but the backend still constructs and
emits them; direct `AddEvent` paths can also bypass the bridge into the
journal. Delete the emit calls at source, don't extend the skip list:

- `tool_execution`, `tool_output`, `tool_response` (9 producing files),
  `tool_call_progress`
- 8 cache events (`cache_write` emitted from 17 files across the agent
  core; `cache_hit/miss/expired/cleanup/error/operation_start`,
  `comprehensive_cache`)

Naming bug: the backend emits `comprehensive_cache`, the bridge skips
`comprehensive_cache_event`, and the frontend renders
`comprehensive_cache_event` — three spellings, none matching. The emitted
event is invisible and unskipped: it rides the bus and the journal for
nobody. Delete or fix the spelling plus the dead renderer.

Write-only telemetry — emitted, never read; demote to logs/metrics, not bus
events (consistent with the persistence boundary above):

- `json_validation_start/end`, `llm_messages`, `llm_token_usage`,
  `error_detail`, `performance`
- `mcp_server_connection_start/end`
- `streaming_connection_lost/error/progress`

Keep as live-only — genuinely consumed by Execution Logs and live views,
but must never enter the chat journal (~40 types: streaming, progress,
workflow-step internals, orchestrator boundaries, MCP status, etc.).

Open allowlist items from the restore inventory (not yet resolved):

- `input_requested` needs kind + payload schema + requested→resolved
  pairing, or refresh resurrects answered approvals as pending.
- Context-lifecycle markers (summarization, max-turns/limits) need an
  explicit home (`assistant_progress` kind vs new marker type).
- In-chat step collapse/expand depends on orchestrator/step boundary
  events; moving them to Execution Logs is a UI change to acknowledge.
- `background_agent_started` schema must carry what delegation cards
  render (id, instruction, depth, model, servers, template).

Observed-UI mapping (2026-09-22) — what the current transcript actually
shows, via `TerminalEventTranscript` (payload-based components first,
`EventDispatcher` as fallback):

- User bubbles + `AssistantTranscriptMessage` (payload fields, not event
  types) for all assistant carriers.
- Minimized tools: `ToolCallCard` behind a disclosure, built by
  `pairToolCalls` from exactly `tool_call_start/end/error`, paired by
  `tool_call_id`. The canonical schema must preserve that pairing.
- Turn completion/failure cards, delegation/bg-agent cards, input
  requests when pending, `large_tool_output_*` artifact rows.

That is ~8 visible families — fewer than the 12 canonical types, so the
allowlist covers the screen with room to spare. Conversely,
`tool_call`/`tool_result`/`tool_execution`/`tool_output` reach no
renderer even in the new transcript and join the delete/demote list, not
the canonical one. Token/context widgets have reachable renderers but no
events arrive on retained-CLI tabs (CLIs report no per-turn usage), which
confirms keeping them out of the journal as ledger/diagnostics data.
