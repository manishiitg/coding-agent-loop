[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-106 — concurrent Chat and Schedule tabs can display events from the wrong session

| Field | Value |
|---|---|
| Status | `partially implemented` — **root cause identified in the backend and repaired 2026-08-15** (Codex retained-answer lookup now binds to the exact thread/rollout); frontend ownership guards and the session-switch render reset are also in; runtime verification pending |
| Priority | P0 |
| Owner | frontend session-event ownership and workflow-tab isolation |
| Reported | 2026-08-15 |
| Related | [PLAT-020](../coding-agent-bridge/plat-020.md)), [PLAT-095](../scheduler-runs/plat-095.md)), [PLAT-103](../coding-agent-bridge/plat-103.md)), [PLAT-104](plat-104.md) |

## Problem

When a workflow Schedule and an interactive Chat are open concurrently, the
formatted Chat can display an assistant update produced by the Schedule. This
makes a truthful Schedule message look like the answer to the user's unrelated
Chat question.

This is more serious than duplicate rendering: the UI is violating the session
boundary. A user cannot tell which automation produced an action, warning, or
answer.

## Evidence

The Build in Public reproduction used Schedule session
`schedule-cron--51af4f19_1786764627816018000`. The displayed sentence

> The receipt call returned an opaque transport failure. I’m checking durable
> Pulse state before any retry so I don’t create a duplicate terminal record.

exists in that Schedule's captured turn at
`agent_go/logs/agent_prompts/schedule-cron--51af4f19_1786764627816018000/stream_turn-000_attempt-0_040109.txt`.
The user's interactive Chat text does not exist in the Schedule prompt logs.
Therefore the command was not misrouted to Pulse; a Schedule event was rendered
under the Chat tab.

PLAT-104 is adjacent but not sufficient. It covers HTTP and SSE creating two
copies of one message **within one session**. This issue is an event from session
A becoming visible while session B is selected.

## Required repair

1. Key every event subscription, cache page, optimistic record, stream fragment,
   and terminal selection by the exact tab `session_id`.
2. At event ingestion, require the requested session, transport envelope session,
   and event owner session to agree. Route an event only to its owning session;
   never rebind it to the currently selected workflow or main terminal.
3. On a tab/session change, synchronously reset the selected terminal and visible
   event source before loading the new session. Stale data may remain cached under
   its original session but must never render during the transition.
4. Keep Chat and Schedule as independent tabs even when they share a workflow.
   Workflow identity is not conversation identity.

## Root cause — corrected 2026-08-15

**The frontend was never able to detect this event.** By the time it arrived it
already carried the Chat session's `session_id`, terminal ID, and execution ID:
the backend had attributed the answer to the requesting session. Session-scoped
guards in the UI are therefore defence in depth, not the fix.

The defect is in Codex retained-answer lookup.
`codexcli.ReadRetainedTurnMessages` resolved a transcript through
`findCodexRolloutForTurn(turnStart, workingDir)`, which walks `~/.codex/sessions`,
keeps every rollout modified since `turnStart − 30s`, **sorts by modification
time descending, and returns the first whose `session_meta.payload.cwd` matches**.

A workflow's interactive Chat and its scheduled run execute in the *same*
working directory. Both rollouts match the `cwd` test, so the lookup returns
whichever conversation wrote most recently — routinely the other one. The
selected answer was then returned to the requesting session and stamped with its
identity, which is exactly why the Build in Public reproduction found the
sentence in the Schedule's prompt log while the UI rendered it under Chat.

Working directory is not a conversation identity. Codex's identity is the
thread/rollout ID: it names each file `rollout-<timestamp>-<thread-id>.jsonl` and
repeats the same value in `session_meta.payload.id`.

### Repair

`multi-llm-provider-go/pkg/adapters/codexcli`:

- `codexInteractiveSession` gains `threadID` and `rolloutPath`, pinning a session
  to its exact conversation;
- new `codexcli_rollout_binding.go` resolves in order — (1) a pinned thread ID,
  re-resolved through `findCodexRolloutForThread` so it survives file rotation;
  (2) otherwise a directory/recency match that **excludes rollouts already
  claimed by other live sessions**, which is what prevents two sessions in one
  directory from selecting the same transcript before either has learned its ID;
- the thread ID is recorded on first resolution and also bound as soon as the
  interactive adapter discovers it (`readCodexTranscriptUsage`), so the steady
  state is always the exact path;
- `readCodexRolloutFinalAssistantText(path, turnStart)` reads ONE known rollout,
  so no directory guess participates in choosing whose answer is returned;
- `ReadRetainedTurnMessages` uses the bound path.

Coverage in `codexcli_rollout_binding_test.go`: a Chat lookup returns the Chat
answer when the Schedule wrote more recently to the same directory; the
unbound directory rule is shown selecting the newest file (documenting why
binding is required) and the exclusion set correcting it; and a pinned session
keeps its rollout when a newer foreign one appears. `go build ./...` and all
`pkg/adapters/...` tests pass.

## Frontend defence in depth — 2026-08-15

These guards do **not** fix the reported leak — see Root cause above; the leaked
event already carried the Chat session's identity. They close the separate class
where an event still declares a foreign owner, and they harden the tab
transition. They were written before the backend cause was found and are kept as
defence in depth.

**Repair 1 was already satisfied.** `tabEvents` is keyed `Record<sessionId,
PollingEvent[]>` and SSE connects per session (`/api/sessions/{id}/events/stream`).
Keying was never the defect.

**Repair 2 was the actual hole, and is now closed.** The only ingestion filter,
`retainEventInSessionWorkingSet`, classified events by *cost* (child transcript
detail vs. session lifecycle) and never compared the event's owning session to
the bucket it was being written into. It also failed open:

```ts
const terminalId = event.terminal_id?.trim()
if (!terminalId) return true        // accepted anything without a terminal
```

Meanwhile `ChatArea.tsx` derives `actualSessionId = response.session_id ||
sessionId` — the transport envelope — and never cross-checks each event's own
owner. An event owned by session A arriving under an envelope labelled B was
therefore written into B.

This explains the exact symptom rather than merely being adjacent to it:
`unified_completion`, `agent_end`, and `conversation_end` are **absent** from
`CHILD_TRANSCRIPT_DETAIL_EVENT_TYPES`, so a *finished assistant answer* passed
the filter unconditionally while noisy streaming chunks were dropped. That is
why a completed Schedule sentence surfaced in Chat.

Changes:

- new `eventBelongsToSession(sessionId, event)` in
  `frontend/src/utils/sessionEventWorkingSet.ts` — the ownership boundary,
  deliberately separate from the volume filter;
- `retainEventInSessionWorkingSet` now checks ownership **first** and never
  fails open for a foreign event, so every `addTabEvent` / `addTabEvents` /
  `_addTabEventsImmediate` write path is covered at one chokepoint;
- `handleLiveStreamingEvent` in `ChatArea.tsx` returns early for a foreign
  event, so streaming text is covered too (the ticket names streaming text, and
  it is separate state from `tabEvents`).

An event that declares **no** `session_id` is still accepted: optimistic local
records and legacy events carry none, and rejecting those would silently drop
the user's own messages. Only a *disagreeing* owner is rejected — which is the
only case that can cross a session boundary.

Regression coverage in `sessionEventWorkingSet.test.ts` was verified to fail
against the previous behaviour before the guard was added (5 failures), then
pass with it. The full frontend suite passes (476 tests).

**Repair 3 is now implemented.** Two stale-render vectors existed, both because
the reset happened one render too late:

- `TerminalCenter` cleared `terminals` / `selectedID` / `userSelectedID` in a
  `useEffect` keyed on `currentSessionId`. Effects run *after* render, so the
  first render for the newly selected tab still painted the previous session's
  terminals and selection. The reset now also runs **during render** via React's
  documented "adjust state when a prop changes" idiom, so React discards that
  render and re-renders with cleared state; stale terminals are never committed.
  The effect is retained for the ref/non-state cleanups it also owns.
- `ChatArea.displayEvents` kept a ref-stability cache (`displayEventsRef`) whose
  reuse test is `length + first ID + last ID` — a same-session heuristic that
  says nothing about ownership — and it was never cleared on a session change.
  The cache is now stamped with the session it belongs to and dropped when that
  changes, with `activeSessionId` added to the memo dependencies.

**Concurrent-session coverage added** in
`frontend/src/stores/useChatStore.sessionIsolation.test.ts`, following the P0
acceptance directly: distinct user/assistant/tool/completion events in both a
Chat and a Schedule session for one workflow (2, 3); 25 interleaved rounds with
deliberate cross-envelope writes on every other tick while both stream (4); and
a late history page for the Schedule session delivered while Chat is selected
(5). Assertions are on exact event IDs and session ownership, never message text
(6). Three of the four were verified to fail against the previous behaviour
before the guard was added. Full frontend suite passes (481 tests).

**Still required before this can be marked implemented:** runtime verification
of the *backend* repair — a real concurrent Chat + Schedule run on one workflow,
confirming each session's retained answer comes from its own Codex thread. Unit
tests prove the binding contract with synthetic rollouts; they do not prove a
live Codex process behaves as modelled. The store-level test likewise proves the
ownership contract, not that the live UI holds the invariant while rapidly
switching real streaming tabs (acceptance 4).

Do **not** mark this fixed or commit it as fixed until that live verification
passes.

## P0 acceptance

1. Start Schedule session A and interactive Chat session B for one workflow.
2. Emit distinct user, assistant, tool, progress, and completion events into both.
3. Selecting B shows no event from A; selecting A shows no event from B.
4. Switch rapidly while both sessions stream and while older history is loading;
   the invariant still holds.
5. Reload and resume both tabs; no event migrates or duplicates across sessions.
6. The test asserts exact event/session ownership, not message text heuristics.
