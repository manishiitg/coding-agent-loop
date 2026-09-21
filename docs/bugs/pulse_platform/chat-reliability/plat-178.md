[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-178 — retained chat delivery and transcript recovery can leave the chat UI behind the terminal

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `two additional recurrence fixes pushed to main; deployment and fresh-client verification pending` |
| Last synchronized | `2026-09-21` |

- **Priority:** P1 — real conversation data loss, user-visible and
  confusing (the agent appears to "forget" recent work and asks the user to
  re-explain things it already did), and the recovery source needed to fix
  it is already sitting on disk unused.
- **Owner:** conversation persistence (`cmd/server/chat_history_persistence.go`,
  `cmd/server/server.go`'s full-turn save block), terminal/session restore
  (`cmd/server/chat_history_routes.go`), Claude Code transcript reader
  (`multi-llm-provider-go`'s `claudecode_transcript_messages.go`).
- **Related:** [PLAT-177](../coding-agent-bridge/plat-177.md)) (same session, same resume boundary,
  different symptom — tool-access confusion vs. this ticket's conversation
  data loss; investigated together, filed separately since the root causes
  are unrelated).

## Symptom

On workflow "substack" (session `b5e39872-4e4e-4645-8059-6d6e7a1231db`), the
user resumed a chat and the agent presented an old assistant message ("Yes —
both bugs you reported are fixed... I also added a new 'Follow Health'
panel... still want to check the `step-connect-creators` wording?") as if it
were the current end of the conversation. It was not — the user had
continued the conversation well past that point. The user: "this is not the
final assistant response.. this is like the 2nd msg... what don't we resume
properly."

## Root cause, confirmed by direct evidence

**1. `conversation_history` is a one-shot snapshot, written only when a full
turn completes — not continuously as a live tmux CLI session runs.**

The save happens once, at the end of the streamed `/api/query` handler,
after a turn completes:
- `[CONVERSATION DEBUG] Native continuation merge: ...` — `cmd/server/server.go:6233`
- `[CONVERSATION DEBUG] Final save: ...` — `server.go:6236-6238`
- `[BUILDER LOG] Saved conversation log (752 messages) to
  Workflow/substack/builder/conversation/2026-08-22/session-b5e39872-...-conversation.json`
  — `server.go:6358`, inside the `isWorkflowPhase` block (`server.go:6326-6363`).

Live: this fired at `13:51:26` on 2026-08-22, saving exactly 752 messages
ending with the "Follow Health panel" reply. The file's mtime on disk is
`13:51:26` and it was never written again.

A structurally identical one-shot writer exists for background "synthetic"
turns (`cmd/server/background_agents.go:2394-2435`). Neither path is
periodic; the only ticker in this area
(`conversation_turn_lifecycle.go:317`, 250ms) is a scheduled-message
completion poller, unrelated to persistence.

**2. Once a session is "warm" (a live tmux pane already exists — exactly the
state right after any resume), further messages go through `/api/live-input`
instead of a full turn, and that path only ever persists the user's own
outgoing message, never the assistant's reply.**

`/api/live-input` (`server.go:8342-8477`) delivers directly into the
retained tmux session (`retainedSession.Send`,
`deliverRetainedMainTerminalInput`, `mcpagent.DeliverAgentInput`) whenever
`sessionHasLiveMainCodingTmux` is true (`server.go:8390`) — bypassing the
full-turn handler entirely, so it never reaches the save block in finding
#1. Its only persistence hook is
`appendLiveInputToPersistedChatHistory` (called at `server.go:8353,8370`,
defined `chat_history_persistence.go:2934-3009`), which reads the existing
JSON, and appends **only**:

```go
history = append(history, llmtypes.MessageContent{
    Role:  llmtypes.ChatMessageTypeHuman,
    Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: message}},
})
```

(`chat_history_persistence.go:2976-2979`) — hardcoded
`ChatMessageTypeHuman`. The CLI's assistant replies to these live-input
turns are never written back. Live: the user's session went straight into
this warm state after the 13:51:26 save (confirmed by
`sessionHasLiveMainCodingTmux`-gated CDP browser activity at `13:55:04` and
raw terminal polling through at least `14:31`), so nothing after 13:51:26
ever reached `conversation_history` — even though real, substantial
conversation happened.

**3. When the tmux pane later dies and the session needs to restore, the
fallback tier trusts that same stale snapshot with no attempt to recover
anything richer.**

`chat_history_routes.go:155-176` restore logic: tier `attach_existing`
(`chat_history_routes.go:250-271`) tries to reattach to the live tmux pane
via `captureTerminalPane`; live, this failed at `16:18:20` with
`reason=tmux_session_not_running` (the pane was gone by then). It falls
through to tier `persisted_snapshot`
(`chat_history_routes.go:273-301`/`selectPersistedTerminalSnapshot` at
`chat_history_routes.go:303-314`), which reads only the `terminal_snapshots`
already embedded in that same stale conversation JSON — i.e. the
13:51:26 state. No tier consults tmux scrollback, a separate transcript
log, or the CLI's own on-disk session file; the comment at
`chat_history_routes.go:141-153` explains the two launch-based recovery
tiers are deliberately skipped here (a real, separate constraint — avoiding
a tool-registration race by deferring launch to the next `/api/query`) —
that constraint is legitimate but unrelated to *this* gap, and no other
tier was added to compensate for it.

**4. The full, missing conversation genuinely exists on disk — the restore
path just never reads it.**

Claude Code CLI writes its own transcript independent of agent_go's JSON,
at `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`
(`multi-llm-provider-go/claudecode_transcript_path.go:10-20`). Confirmed
live at
`~/.claude/projects/-Users-mipl-ai-work-mcp-agent-builder-go-workspace-docs-Workflow-substack/71ed3dcb-fe3c-412d-94cb-e62226fc4648.jsonl`
(`external_session_id` from the session's own `runtime.agent_session_handle`)
— **155 entries timestamped after 13:51:26**, including 36 real user
messages, 63 real assistant messages, 37 attachments, and 3 system entries,
running until a final `<local-command-stdout>Bye!</local-command-stdout>`
entry at `15:19:59` local time. None of this reached
`conversation_history`; all of it is still sitting in this file, unread by
the restore path.

`readClaudeTranscriptMessages`
(`multi-llm-provider-go/claudecode_transcript_messages.go:43-134`) already
knows how to reconstruct full `[]llmtypes.MessageContent` (text, tool_use,
tool_result) from exactly this file format, but it is only ever called from
`claudecode_interactive_adapter.go:664`, scoped to a single turn's
`turnStart..now` window — never from the resume/restore path.

## Why this matters beyond the one incident

Any session that (a) resumes into a warm/retained tmux state — which is the
normal case, not an edge case, since a resumed session almost always still
has its tmux pane alive initially — and (b) exchanges any further messages
purely through `/api/live-input` before its tmux pane eventually dies for
any reason (intentional `/bye`, crash, terminal reap, host restart) will
have that entire stretch of conversation permanently invisible to
`conversation_history`, `costs`, and every UI/API surface that reads it —
even though Claude Code's own transcript still has it. This is not specific
to the substack workflow or to this one incident.

## Fix implemented

Four independent gaps were identified. All four are now implemented:

1. **Persist every provider-confirmed live-delivery user message.** Both
   `/api/query` retained delivery and `/api/live-input` now call the same
   `persistLiveInputUserMessage` helper after the coding CLI confirms receipt.
   This closes the 2026-09-16 Confida recurrence where session
   `61247bd2-347b-4776-982f-e158e84f9ad6` delivered the user's message to Pi
   and completed in the terminal, but the durable JSON stopped before the last
   two turns. The helper is reached by durable `mcpagent`, cold retained-tmux,
   and running-Agent delivery paths and keeps the authenticated internal user
   ID when writing history.
2. **Make the workflow-builder restore path read the native transcript**
   (finding #3/#4) — **implemented**. `restoreLatestBuilderConversation`
   (`workflow_builder_session_routes.go:215-`, the actual function that
   serves the stale snapshot to the chat UI when no live in-memory session
   matches — confirmed to be the real serving path, not the terminal-pane
   tiers in `chat_history_routes.go`, which restore the separate raw
   terminal widget) now calls
   `refreshLatestBuilderConversationFromNativeTranscript`
   (`claude_native_transcript_sync.go`, new file) on the winning candidate
   before converting it to display events. For `provider=claude-code`
   sessions with a resolvable native session ID and working directory (read
   from the snapshot's own `runtime.agent_session_handle.provider` block),
   it locates Claude Code's own JSONL transcript
   (`~/.claude/projects/<slug>/<session-id>.jsonl`, using the same
   working-directory-to-slug escaping scheme as
   `multi-llm-provider-go/claudecode_transcript_path.go`, duplicated rather
   than imported since that resolver is unexported), reads the full native
   transcript, and sequence-merges it with persisted history. It does **not**
   use the snapshot's `updated_at` as a transcript cursor: persisting a later
   live-input human message advances that field before the preceding assistant
   reply is saved, so a timestamp cutoff can permanently skip the reply and
   duplicate the later user message. The merge preserves persisted-only prefix
   messages, uses shared ordered messages as anchors, inserts native-only
   replies between them, and deduplicates messages present in both sources. It
   keeps only real chat text (plain-string user content, `text`-typed assistant
   blocks — `tool_use`/`tool_result`/`thinking` are filtered out as execution
   detail, not something either party said), and best-effort persists the merge
   back to the same file so later reads see it too. A failed persist (e.g.
   the workspace API being unreachable) is logged but never blocks the
   restore itself — the in-memory catch-up is still served.

   The Pi branch now resolves its native transcript with
   `picli.ReadNativeTranscriptFromWorkingDir(workingDir, nativeSessionID)`.
   The earlier default-store lookup could not see transcripts inside the
   session's isolated runtime directory, so completion synchronization retried
   three times and found no completed reply even though the JSONL contained the
   missing user message and full assistant answer.

3. **Preserve Pi thinking blocks as retained-turn progress.** A second live
   check on the same Confida session used the message “can you group them by
   companies”. The terminal showed many narrated updates, including
   “Identifying Session Clues” and “Mapping Company IDs”, while the chat showed
   only “41 tool calls” until the final answer. Direct inspection of Pi's JSONL
   proved those updates were `thinking` blocks paired with tool calls and empty
   `text` blocks. Normal Pi streaming already maps `thinking_delta` to visible
   assistant progress, but `ReadRetainedTurnProgressMessages` rebuilt progress
   only from `text`, silently dropping the retained equivalent. The transcript
   summary now keeps a separate `ProgressMessages` projection containing both
   thinking and text blocks in order. The final-answer `Messages` projection is
   unchanged, so thinking can never satisfy retained-turn completion or appear
   as the final answer.

4. **Always reconcile interactive workflow chats with durable history.** A
   later reproduction in the same Confida session exposed a frontend-only
   restore race after the backend fixes above had correctly persisted the
   answer. The terminal contained the completed judge scorecard (`PASS`,
   `0.95 / 1.0`, seven Langfuse scores and `judge_scorecard.md`) and the durable
   `conversation_history` also contained that answer plus the next
   `dont add into langfuse` exchange, while Chat stopped at the older
   “workflow execution is currently running” response. The persisted
   `ui_events` array was capped at 200 and ended before those durable turns.
   Workflow restoration treated any non-empty volatile event array as fully
   hydrated, so it never requested the authoritative conversation history.

   Interactive workflow tabs now reconcile with durable history even when a
   volatile event tail is present; `hydrateTabEvents` then deduplicates and
   merges that live tail so tool/progress events remain visible. Entering
   Formatted view also performs the same reconciliation, covering refreshes
   that initially reopen in Terminal view. Read-only schedule tabs retain the
   cheaper empty-only hydration rule.

## Why this required several fixes

The user-visible symptom — “the terminal has the answer but Chat lost it” —
can be produced independently at four ownership boundaries:

1. the retained coding-agent delivery path can accept a user message without
   appending it to the server's durable chat;
2. the provider's native transcript can contain the completed answer while
   server recovery looks in the wrong working directory;
3. the provider can represent visible progress as `thinking` rather than
   `text`, causing retained progress reconstruction to omit it; and
4. the server's durable chat can already be correct while the frontend treats
   a non-empty, bounded `ui_events` tail as a complete transcript and never
   reconciles the durable messages.

These are separate data paths that converge on the same screen, so closing one
does not prove the other three are closed. The invariant established by this
ticket is: the provider transcript is recovery evidence, durable
`conversation_history` is authoritative chat state, and `ui_events` is only a
bounded execution/progress projection. A non-empty `ui_events` array must never
suppress durable-history hydration.

## Deployment and live verification

- Deployed application revision
  `912b9347e473d078f3c82b72c1bf10a9fd1ebedc` as Confida release
  `confida-912b9347-20260916083842` on 2026-09-16. The release pipeline
  reported the agent API healthy, the public endpoint returned HTTP 200, and
  the `confida-agent`, `confida-gateway`, and `confida-workspace` user services
  were active after activation.
- Live browser verification after a hard reload restored the durable Chat
  history rather than stopping at the stale capped UI-event tail. The UI
  displayed the previously missing local-only Langfuse configuration reply
  and the complete trace/business-context response. This confirms the deployed
  Formatted-view hydration is reading and merging authoritative history.
- The same reload also confirmed the bundled toolbar changes: Knowledgebase is
  top-level, Costs is under Ops, and Playbooks is under Setup.
- Provider-matrix verification covers every registered persistent live-input
  CLI: Claude Code, Codex CLI, Cursor CLI, Pi CLI, and Muse. Shared user-message
  persistence and frontend durable-history reconciliation are provider-neutral;
  each provider has retained-progress/final-answer readers and a native
  transcript recovery branch. Focused adapter, retained-session, and server
  recovery suites passed across all three repositories on 2026-09-16. A
  contract-derived regression now fails if a future persistent live-input CLI
  is registered without declaring transcript reading or without being included
  in native chat-history recovery.

## 2026-09-16 13:40 IST recurrence report

Conversation `e4f5abb1-57f3-4ba8-823e-87b3fe2e2a93` is a direct reproduction
of gap #4 after the fixed release had reached the server. The operator
confirmed the page had been refreshed, ruling out a pre-deployment frontend
bundle still running in memory:

- At 13:40:07 IST, the Formatted view stopped at intermediate progress
  (“Defining Post-Execution Report”, “Defining Next Steps”, and “Testing the
  Implementation”). At 13:40:21 IST, the Terminal view already displayed the
  completed response.
- The production conversation JSON had been updated at 13:38:31 IST and held
  257 durable `conversation_history` messages, including the complete final
  “I have created the dedicated scripted step...” answer. Its 43 `ui_events`
  ended earlier. The answer therefore was not lost by the server; the formatted
  projection was stale because it trusted the non-empty UI-event tail.
- Revision `912b9347e473d078f3c82b72c1bf10a9fd1ebedc`, which implements the
  unconditional interactive-chat reconciliation, was packaged as
  `confida-912b9347-20260916083842`. Confida's release names and filesystem
  timestamps use the host's UTC+02 timezone: 08:38 host time is 12:08 IST,
  about 91 minutes before the screenshots — not 14:08 IST as initially
  recorded. The current release, `confida-7def0915-20260916090324`, was active
  by 12:36 IST and includes that revision.
- The follow-up reconciliation has an ordering defect. `hydrateTabEvents`
  restores durable conversation events first, filters duplicates from the
  in-memory event response, and then calls `addTabEvents` with the remaining
  volatile tail. `addTabEvents` is append-only and Formatted view preserves
  array order. Therefore older, non-carrier progress events can be appended
  *after* the newer durable final answer. The newest answer is no longer the
  bottom item even though hydration fetched it successfully, producing the
  same apparent “final message lost” symptom.
- This incident has the exact dangerous shape: the 43 persisted UI events end
  on the earlier 13:00 IST turn, the 257-message durable history ends with the
  13:38 final answer, and the screenshot bottom shows intermediate progress
  rather than that final answer. The existing regression test checks that
  durable history is requested, but does not check final ordering after a
  non-carrier live progress tail is merged.

Classification: **confirmed post-deployment frontend regression; no backend
message loss.** The first PLAT-178 frontend fix corrected the decision to fetch
durable history, but did not enforce chronological/turn-relative ordering when
combining that history with the live event tail. The follow-up must add an
ordering regression test with a durable final answer plus older live progress,
then merge the progress before its matching final carrier rather than blindly
appending it.

## Follow-up reconciliation refactor

Implemented and deployed on 2026-09-16:

- `hydrateTabEventsFromConversation` is now the single ordering boundary for
  persisted UI events, raw events already received through SSE, and the current
  EventStore window. All three sources enter `conversationToRestoredEvents`
  together, so its turn anchors and chronological projection apply once.
- Removed both post-hydration append paths: normal page hydration and the
  existing-tab restore path no longer call `addTabEvents` after constructing
  durable history. Legacy sessions without durable history keep their existing
  live-only fallback.
- Repeated hydration filters its own synthesized events before rebuilding the
  trace, preventing generated timestamps from feeding back into the next
  projection.
- Durable history still paints before a slow live-status request completes;
  when the live response arrives, the same projection function reconciles it.
- The shared carrier extractor now handles both nested `{data:{content}}` and
  legacy flat `{content}` event envelopes after restore metadata is attached.
  The broader product fallback suite exposed this pre-existing deduplication
  hole as a duplicate user row during the refactor.
- Added regressions for the production ordering shape (older live progress plus
  a newer durable final answer) and flat-envelope carrier deduplication.

Verification: 53 focused restore/conversation tests pass, TypeScript project
compilation passes, focused ESLint passes, and `git diff --check` is clean.
Commit `84a102b2c4de39c1472d3ad7eda686c281257df9` was pushed to `main` and
deployed as Confida release `confida-84a102b2-20260916134610`. The deployment
pipeline passed release-asset and bundle-budget checks; `confida-agent`,
`confida-gateway`, and `confida-workspace` were active; internal agent health
and the public endpoint returned 200; workspace health was `healthy` with the
Landlock sandbox available. A hard-refreshed fresh retained turn remains the
final user-visible verification.

## 2026-09-21 queued-turn recurrence

A Sheets Analysis chat showed the terminal and Formatted Chat disagreeing about
the same retained Claude session. In project chat session
`work:project:be2a4cf4-0f3b-49f7-a3fb-084f5f8028c4` (Claude native session
`ce286440-2463-442d-84f6-fbce00e88e2a`, terminal
`mcp-agent-20260921-001313`), the terminal chronology correctly showed the user
asking "si sheet111 tab used for anything?" between two assistant turns. The
durable conversation omitted that user prompt: its final eight
`conversation_history` entries were all assistant messages, while the capped
UI-event tail ended before the prompt. Formatted Chat therefore grouped stale
assistant updates around the newest reply and made old content appear current.

The native Claude JSONL retained the missing boundary, but represented it as a
human `queued_command` attachment rather than a normal `user` entry: a
`queue-operation` enqueue was followed by an attachment with
`type: "queued_command"`, `humanTurn: true`, and `origin.kind: "human"`, then
the final assistant response. The backend transcript reader accepted only
normal `user`/`assistant` entries, so it silently discarded the queued user
turn and merged consecutive assistant updates across the lost boundary.

Two complementary fixes are now on `main`:

- `44c727fc3` makes restored event identities content-bound and only applies
  the `prependedIndex` shift when the old tail was actually preserved. This
  prevents React/Virtuoso row reuse from displaying stale content when a
  bounded history page moves during hydration.
- `eb92d4570` recovers only genuinely human `queued_command` attachments,
  unwraps their `<pasted_content>` payload, ignores non-human attachments, and
  inserts the recovered user message into sequence merging so the turn
  boundary cannot disappear.

Regression coverage includes the parser and sequence merge for the missing
human boundary, restored-event identity changes, and bounded-history scroll
reconciliation. Focused Go tests pass; all 31 frontend
`sessionRestore`/`useTranscriptScroll` tests pass; lint, Go build, workspace
build, and Electron build pass. Both commits are pushed to `main`; deployment
and a fresh-client retained-turn check remain pending.

## Out of scope

- Not investigating why the tmux pane died in this specific incident — the
  transcript's own last entry (`<local-command-stdout>Bye!</local-command-stdout>`)
  suggests an intentional `/bye`/exit rather than a crash, but the fix
  needed here is the same regardless of why the pane ends.
- The existing tool-registration-race avoidance already documented at
  `chat_history_routes.go:141-153` is untouched — this fix lives entirely
  in the workflow-builder conversation-display path, a different function
  than the terminal-pane restore tiers that comment describes.

## Verification

The original PLAT-178 repair completed build, unit, deployment-health, and live
hard-reload verification. The follow-up reconciliation refactor above is now
deployed and healthy; it still requires a hard-refreshed fresh retained-turn
check before this recurrence can be closed.

- `go build ./...` clean.
- 2026-09-16 regressions pass:
  `TestTryDeliverQueryAsLiveInputReactivatesSettledRetainedTmux` proves a
  confirmed `/api/query` retained delivery persists the exact authenticated
  user/session/message tuple, and
  `TestRefreshLatestBuilderConversationFromPiTranscript` proves recovery reads
  the isolated working-directory transcript rather than ambient Pi storage.
- `TestPiRetainedProgressPreservesMessagesAroundTools` now includes a real
  thinking-only assistant record (`thinking` + tool call + empty text), proves
  it is emitted exactly once as progress, and proves it never enters the
  final-answer message stream. The shared retained-progress and completion
  watcher suites also pass for all retained providers.
- Frontend regression `workflowTabsNeedingHydration` proves an interactive
  workflow-builder tab with a non-empty capped volatile tail still requests
  durable history, while a populated read-only schedule remains untouched.
  The focused session-restore/hydration suite passes (19 tests), along with
  TypeScript compilation and focused ESLint.
- New tests in `claude_native_transcript_sync_test.go`: working-directory
  slug encoding matches the real scheme; text extraction correctly keeps
  plain-string/`.text`-block content and drops `tool_use`/`tool_result`/
  `thinking`; a fail-before/pass-after style test constructs a snapshot
  frozen at `13:51:26` plus a native-transcript fixture with two later
  messages and confirms the merge appends both in order and advances
  `updated_at` to the transcript's newest timestamp; a no-op test confirms
  non-`claude-code`/missing-runtime snapshots are left untouched.
- `TestRefreshLatestBuilderConversationIgnoresLiveInputUpdatedAtAsTranscriptCursor`
  reproduces the real ordering gap: persisted `user1,user2` with `updated_at`
  after native `assistant1`, and verifies restore returns
  `user1,assistant1,user2,assistant2` without duplication.
- Merge-level tests cover missing replies between persisted live inputs and
  repeated identical user messages with an older persisted-only prefix.
- `go test ./cmd/server/...`: all new tests pass; the only failures
  (`TestWorkshopResolveLLMConfigExpandsCodingAgentMode`,
  `TestStandalonePulseReviewCommandsUsePersistedReviewerPipeline`,
  `TestArtifactDriftAuditsTheSchedule`) are pre-existing on `origin/main`
  and unrelated to conversation persistence — confirmed by running the same
  three tests against `origin/main` directly before this change.
- Root cause itself was independently re-verified by direct evidence before
  any code was written: `conversation_history` JSON's mtime (`13:51:26`)
  confirmed frozen while the live session continued (CDP browser call at
  `13:55:04`, terminal polling through `14:31`);
  `appendLiveInputToPersistedChatHistory`
  (`chat_history_persistence.go:2934-3009`) read directly, confirmed it
  only appends the human message; the native Claude Code transcript file
  was located on disk and read directly — 1774 total lines, 155 entries
  timestamped after `13:51:26` local (36 user, 63 assistant, 37 attachment,
  16 queue-operation, 3 system), ending `15:19:59` local, proving the
  "lost" conversation was fully intact and just unread by the restore path.
