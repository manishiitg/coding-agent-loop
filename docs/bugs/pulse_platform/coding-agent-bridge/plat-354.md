[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-354 — Show Muse native multiple-choice questions in chat

| Field | Value |
|---|---|
| Status | `proposed; not implemented` |
| Priority | P1 interactive chat correctness |
| Owner | coding agent bridge and frontend chat |
| Reported | 2026-09-23 |
| Related | PLAT-134 (browser products cannot answer native terminal questions) |

## Problem

Muse can call its native `request_user_input` tool with several questions and
multiple options per question. The current tmux adapter reads the visible
question widget, moves the cursor to option 1, and presses Enter automatically.
The user never sees or chooses the options in chat. The P0 test
`TestMuseCLIRealAutoFirstOptionThreeQuestionsP0` verifies this current behavior;
it does not test a user-facing choice flow. The option name
`WithAutoSelectRecommended` is historical: the implementation chooses the
first option even when a different option is marked Recommended.

## Observed Muse transcript contract

Muse's native `session.jsonl` includes a `user_input_prompt_requested` event
with `prompt_id`, `tool_call_id`, and a `questions` array. Each question has an
`id`, `header`, `question`, and ordered `options`; options have a `label` and
may carry Markdown preview text. A subsequent `user_input_prompt_settled`
event contains the same prompt/tool-call identity, an `outcome`, and answers
with question `id` and `selected_label`. A real three-question transcript was
inspected locally; the P0 test already reads the requested event to verify
the first displayed labels.

The JSONL file is an observation source, not an input API. The current
transcript streamer emits assistant text and tool start/end chunks but does
not project these question events into chat. The auto-answer path instead
parses tmux pixels and sends tmux keys.

## Proposed behavior

1. Tail the active Muse session's `session.jsonl` by sequence. On a new
   `user_input_prompt_requested`, parse the structured questions and present
   all of them, including option labels and previews, in chat. Correlate the
   prompt by native session ID plus `prompt_id` and `tool_call_id` so resumed
   sessions and earlier questions cannot be confused with the active prompt.
2. Disable automatic first-option selection for turns whose choices are
   presented to the user. Keep the turn pending while the user decides.
3. On submission, use the existing Muse tmux input path to navigate the live
   widget and submit the user's selected option for each question. Before
   sending keys, verify that the visible widget matches the expected question
   and option labels; if it has changed or vanished, refresh the prompt
   instead of sending keys against a stale screen.
4. Confirm completion from the matching `user_input_prompt_settled` event.
   Show the recorded selections or an actionable canceled/error state. Avoid
   duplicate submission across reconnects and repeated event reads.

This is a Muse-specific first implementation. Claude has a related structured
source, but its answer record and tmux widget need a provider-specific bridge.

## Claude transcript follow-up (2026-09-23)

Local Claude conversation JSONL files contain assistant `tool_use` blocks named
`AskUserQuestion`. Their `input.questions` array carries `question`, `header`,
`multiSelect` (when present), and ordered `options` with `label` and
`description`. These blocks have a `tool_use` ID. A later user-role
`tool_result` block refers to that ID and contains the answer as text. The
question row was observed 25 seconds before its matching answer row in one
local session, so the transcript can expose a pending question. Local files
included one-, two-, and three-question calls.

Claude therefore also has enough structured data to render choices without
parsing tmux text. Unlike Muse, it has no observed dedicated
`user_input_prompt_requested` / `user_input_prompt_settled` pair: correlate
`tool_use.id` with `tool_result.tool_use_id`, and treat the result as text
until its answer format is verified. The existing Claude transcript streamer
already projects tool starts and ends, including input arguments; it does not
provide a dedicated user-question event. Claude's native widget still needs
live validation before sending the selected choice through tmux.

## Codex transcript follow-up (2026-09-23)

Local Codex `rollout-*.jsonl` files also persist native question calls as
`response_item` records. A `function_call` named `request_user_input` has
`arguments.questions` with question IDs, headers, text, and ordered option
labels/descriptions. Its `call_id` links to a later
`function_call_output` containing structured `answers` by question ID; a
three-question example was verified locally.

Recent rollouts also show `request_user_input_async` calls. Their observed
arguments use `questions` with `title` and string `options`; the immediate
output was only `{ "accepted": true }`, so that output does not prove which
answer the user eventually selected. The eventual user reply needs its own
correlation rule. Codex rollouts also carry structured turn/task events,
tool-call starts and outputs, assistant messages, reasoning, and token usage.
These shapes differ from both Muse and Claude and should be parsed separately.

## Observed structured-record inventory (2026-09-23)

This is an inventory of record **types**, not conversation content or every
tool name. It comes from 300 local Muse session JSONL files, 2,434 local
Claude conversation JSONL files, the 350 newest local Codex rollouts plus
the older question examples above, and all 927 Cursor `store.db` files found
under the RTS service user's Cursor chat roots. Codex's 13 GB archive was
sampled for this inventory, so older rollout versions may contain other
types. These native formats can change with CLI versions; the lists describe
what was observed, not a promised provider API.

### Muse — `session.jsonl`

Muse records have a `payload_type`, usually with `payload.event.kind`. The
observed `runtime.session` event kinds are:

- Conversation and model: `user_prompt_display`, `assistant_message_committed`,
  `reasoning_committed`, `reasoning_summary_delta`,
  `reasoning_summary_committed`, `model_request_configured`,
  `provider_request_options_configured`, `model_input_trace_recorded`,
  `model_response_created`, `model_completed`, `terminal`.
- Tools and task lifecycle: `proposed`, `accepted`, `scheduled`, `started`,
  `status`, `output`, `tool_delta`, `tool_output_ref`,
  `assistant_tool_calls_committed`, `tool_result_batch_committed`,
  `tool_result_model_visible_content`, `tool_results_cleared`, `completed`,
  `failed`, `cancelled`, `timed_out`, `rejected`, `side_effect_intent`,
  `task_stream_linked`, `task_backgrounded`.
- Human input and approvals: `user_input_prompt_requested`,
  `user_input_prompt_settled`, `requested`, `decision_applied`,
  `stage_requirement_resolved`, `automated_review_started`,
  `automated_review_completed`.
- Context, goals, and usage: `context_block_diagnostic`,
  `context_block_updated`, `context_compaction_candidate`,
  `context_compaction_installed`, `context_projection_checkpoint`,
  `goal_usage_attribution`, `resource_usage_sampled`,
  `todo_snapshot_updated`, `skill_read_observed`,
  `skill_reminder_decision`, `hook_run_started`, `hook_run_terminal`.
- Background work and delivery: `inbox_delivery_anomaly`,
  `inbox_item_queued`, `inbox_item_drained`,
  `memory_reminder_child_session_linked`, `reminder_installed`,
  `reminder_proposal`, `reminder_reconciler_outcome`, `run_retracted`,
  `workflow_child_lifecycle`, `workflow_child_result_protocol_recorded`,
  `workflow_child_result_submitted`, `workflow_launch_reconciled`,
  `workflow_run_launched`.

Other observed Muse `payload_type` families are
`runtime.session.task` (`proposed`, `accepted`, `scheduled`,
`side_effect_intent`, `started`, `completed`),
`tool_batch.effect.started` / `.terminal`,
`approval_wait.effect.started` / `.terminal`,
`reminder.cleanup_effect.started` / `.terminal`,
`runtime.command_intake.received` /
`.session_name.received` / `.settled`,
`subagent.control.attempt_admitted` / `.child_session_bound` /
`.result_ready` / `.resume_context_recorded` / `.runtime_observed` /
`.spawn_accepted` / `.start_attested` / `.status_updated`, and
`async.owner.attempt_admitted`. The observed envelope/fact types without
an inner event kind are `command.invoked`, `run.model.configured`,
`runtime.mcp_tool_identity_catalog`, `runtime.retained_fact`,
`runtime.session.metadata`, `runtime.session.route_facts`,
`runtime.session.task_source_committed`, `runtime.user_intent.accepted`,
`runtime.user_intent.materialized`, `session.end`, `session.name.changed`,
`session.opened.observed`, `session.resource_pressure.observed`,
`session.resumed`, `session.startup_phases.observed`, and
`session.workspace_branch.observed`. A few rows had no `payload_type`.

### Claude — conversation JSONL

Observed top-level row `type` values: `assistant`, `user`, `system`,
`attachment`, `last-prompt`, `atis-latch`, `ai-title`, `mode`,
`permission-mode`, `agent-name`, `custom-title`, `queue-operation`,
`bridge-session`, `file-history-snapshot`, `file-history-delta`,
`cost-state`, `pr-link`, `fork-context-ref`, and `frame-link`.

Within `assistant` and `user` messages, observed content block types are
`text`, `thinking`, `tool_use`, `tool_result`, `image`, `document`, and
`fallback`. A `tool_use` has an ID, name, and input; a later `tool_result`
refers to the ID. `AskUserQuestion` is one tool name, not a dedicated row
type. Some top-level metadata rows are app/session bookkeeping rather than
model conversation events.

### Codex — rollout JSONL

In the 350 newest rollouts, top-level `type` values were `session_meta`,
`turn_context`, `world_state`, `event_msg`, `response_item`,
`token_usage_record`, `compacted`, and
`inter_agent_communication_metadata`.

- Observed `event_msg.payload.type`: `task_started`, `task_complete`,
  `turn_aborted`, `item_completed`, `thread_settings_applied`, `token_count`.
- Observed `response_item.payload.type`: `message`, `agent_message`,
  `reasoning`, `function_call`, `function_call_output`,
  `custom_tool_call`, `custom_tool_call_output`, `tool_search_call`,
  `tool_search_output`.

`request_user_input` and `request_user_input_async` are function-call names
inside `response_item`, not standalone event types. A `call_id` links a call
to its immediate output; for async input the output may only acknowledge
acceptance, as noted above.

### Cursor — RTS `store.db`

RTS Cursor conversations are SQLite stores, principally `meta` and `blobs`,
under the service user's XDG Cursor chat root (with a few legacy
`~/.cursor/chats` stores). The `meta` object had `agentId`,
`latestRootBlobId`, `name`, `mode`, `isRunEverything`, and `createdAt`;
some stores also had `lastUsedModel` or `approvalMode`. It also contains
`blobEncryptionKey`, which must never be surfaced in chat or diagnostics.
The latest root references cumulative message blobs in order, rather than
providing an append-only event sequence. The observed message `role` values
were `system`, `user`, `assistant`, and `tool`; typed content blocks were
`text`, `reasoning`, `redacted-reasoning`, `tool-call`, `tool-result`, and
`image`. `tool-call` / `tool-result` carry `toolCallId`; many calls are a
`CallDynamicTool` wrapper whose `args.toolName` names the actual bridge
tool. Tool results may be strings or structured JSON.

Across all 927 RTS stores, no native question/choice tool name appeared
among tool calls or nested `CallDynamicTool` names. One nested
`human_feedback` call was a platform bridge tool, not evidence of a Cursor
native question widget. This establishes the observed RTS data, not that
Cursor can never emit a native question in another version or configuration.
The existing Cursor adapter already polls `store.db` for assistant text and
tool call/result chunks; commits can lag the visible terminal by seconds.
Separately, Cursor's live `--output-format stream-json` transport has
`system`, `thinking`, `assistant`, `tool_call` (`started` / `completed`),
and `result` event types in the adapter. That stream is a runtime wire
source, not another event table in the RTS `store.db` files inspected here.

### Pi CLI with Gemini — local session JSONL

The local `pi` executable reports version 0.87.0. All 666 local files under
`~/.pi/agent/sessions` were scanned; 660 contain a Google provider or Gemini
model message. Persisted row `type` values were `session`, `model_change`,
`thinking_level_change`, `message`, and `compaction`. `message.role` values
were `user`, `assistant`, and `toolResult`; observed content block types were
`text`, `thinking`, and `toolCall`. Assistant messages carry provider, model,
API, token/cost usage, stop reason, and response ID; tool calls and tool
results correlate by ID and carry arguments/results. No question-like native
tool call appeared in these local session files.

The adapter also creates a separate per-session `markers.jsonl` through its
bundled Pi extension. Its code emits `extension_loaded`, `session_start`,
`agent_start`, `turn_start`, `message_start`, `message_update`, `message_end`,
`tool_execution_start`, `tool_execution_update`, `tool_execution_end`,
`turn_end`, `agent_end`, and `provider_error`. These are adapter-added live
markers, distinct from Pi's native persisted conversation file. Structured
`pi --print --mode json` can stream the corresponding run/message/tool
lifecycle, but that output is a runtime stream rather than the session file.

Confida Hetzner's deployed Pi/Gemini files were **not inspected** on this
pass. The deployment runbook's `~/.ssh/confida_deploy` key is absent locally;
the available Confida key and the default SSH identity were rejected at the
documented SSH port, and port 22 did not respond. The local Pi inventory is
evidence for this workstation only, not for the Confida deployment.

## Acceptance

- A live Muse turn asking three questions in one `request_user_input` call
  shows all three questions and their ordered options in chat without choosing
  any answer automatically.
- The user can choose an option other than the first for each question; Muse
  receives those choices and continues the same turn.
- Chat confirms the answers recorded in the matching
  `user_input_prompt_settled` event. Reload/reconnect does not replay or
  resubmit an already settled prompt.
- A changed or dismissed tmux widget cannot receive keys intended for an
  earlier prompt; the user sees a recoverable state.

## Code references

- `multi-llm-provider-go/pkg/adapters/musecli/musecli_auto_answer.go` —
  current tmux widget parser and automatic first-option submission.
- `multi-llm-provider-go/pkg/adapters/musecli/musecli_auto_answer_live_test.go` —
  three-question P0 and JSONL event parsing.
- `multi-llm-provider-go/pkg/adapters/musecli/musecli_transcript_stream.go` —
  current JSONL-to-stream projection.
