# Refactor spec: unify CLI live-input on tmux-session liveness (drop steer-vs-queue)

**Status:** ✅ core implemented surgically (commit `fc5953e9` on pi-dev-integration). See "What was actually needed" below.
**Scope:** coding-agent **CLI** providers only — `claude-code`, `codex`, `cursor`, `agy`, `pi`. API/LLM agents are untouched.

## What was actually needed (post-implementation note)

On inspection the queue machinery was **already CLI-correct everywhere except one path**, so the change is **12 lines**, not a sweeping rewrite:

- **The only real bug:** `handleSteerMessage` returned **404** ("No running agent") whenever `runningAgents[sessionID]` was momentarily empty *between turns* (object torn down/rebuilt while the tmux session is alive). The UI reads that 404 as "live turn ended" → toast + local queue. **Fix:** when the agent object is absent, call `startNextTurnFromSteer` (it already works without an agent object) before falling through to 404. The CLI resumes its session and processes the message natively.
- **`DeliverUserMessage`** — already never queues for live-input providers (`SendCodingAgentLiveInput`); `AddSteerMessage` is API-only. No change needed.
- **`canSteerSession`** — already falls back to `SessionHasBusyCodingTmux`. No change needed.
- **Frontend toast / `queueStreamingMessage`** — with the 404 race gone, the toast now fires only on genuine transport failures (a real `DeliverUserMessage` error or an uninitialized session with no prior query). That's correct behavior — left as a genuine-failure safety net, not ripped out.

So the sections below are the *original design exploration*; the delivered change is just the liveness-aware `startNextTurnFromSteer` fallback in `handleSteerMessage`. Verified: compiles, steer unit tests pass (one pre-existing unrelated red test on the branch).

## Why

For CLI agents, the server maintains its own *steer-vs-queue* concept on top of a thing the CLI already does natively: a running CLI process owns its own input buffer and decides live-vs-queue itself. Our parallel queue is keyed to the **Go turn-object lifecycle** (`runningAgents[sessionID]`), which blips empty *between turns* — and that mismatch is the source of two live bugs:

- **The 404 → "The live coding-agent turn has ended" toast**: a steer lands in the gap where the Go agent object is momentarily absent, even though the tmux session is alive and ready.
- **"Message queued but never goes"**: once in our queue, flushing depends on fragile turn-boundary logic.

### Empirical justification (this is verified, not assumed)
New real-E2E tests (`…ProcessesQueuedFollowupContract`, in `multi-llm-provider-go`, branch `codex-fix-and-queue-tests`) prove every CLI **natively queues + processes** a mid-turn message delivered straight to its tmux pane (bypassing the server queue):

| Provider | Result |
|---|---|
| claude-code | ✅ processed in-turn |
| cursor | ✅ processed in-turn |
| pi | ✅ processed in-turn |
| codex | ✅ processed as the **next** turn (fine) |
| agy | ✅ processed in-turn (behaviorally confirmed) |

None reject a mid-turn message. So the server queue is redundant for CLI agents.

## Target model

```
deliver(sessionID, msg):
  if provider is a CLI coding agent:
      if tmux session is alive:   send-keys to the pane   # the CLI owns live-vs-queue
      else:                       start a new turn/session with msg as the opener
  else (API/LLM agent):
      keep the existing steer-vs-queue (load-bearing there)
```

**One fork only for CLI agents: is the tmux session alive?** No `runningAgents`-object gate, no 404/409 "no foreground turn", no separate queue, no frontend toast.

## Changes

### 1. Backend — `agent_go/cmd/server/server.go` · `handleSteerMessage` (~6527)
Today it 404s on `runningAgents[sessionID]` being empty and 409s on `!hasActiveTurnCancel && !tmuxBusy`. Replace the CLI path with a **session-liveness** gate:

- If the **tmux session exists** for this `sessionID` → deliver (the existing `runningAgent.DeliverUserMessage(... IntentLiveInput)` / `SendCodingAgentLiveInput`). Deliver **regardless** of `hasActiveTurnCancel` — the pane being alive is sufficient. (`canSteerSession` already half-admits this via `SessionHasBusyCodingTmux`; promote that to the primary gate.)
- If the **tmux session is gone** → `startNextTurnFromSteer(...)` (already exists, ~6653) — start a fresh turn with the message as opener.
- **Remove** the `404 "No running agent for this session"` and `409 "No active foreground turn"` returns for CLI providers. They should be unreachable: alive→deliver, gone→new turn.
- Keep the existing `DeliverUserMessage` error→`409` only as a genuine transport failure (tmux paste failed), not as a routine "turn ended".

> Note: the source of truth for "session alive" is the **tmux session** (via the adapter's session registry / `SessionHasBusyCodingTmux` / a new `CodingSessionAlive(sessionID)` helper), NOT `runningAgents`.

### 2. mcpagent — `agent/message_delivery.go` · `DeliverUserMessage` (~116)
Currently: `if isCodingAgent && SupportsLiveInput → SendCodingAgentLiveInput; else AddSteerMessage` (queue).
- For CLI (`SupportsLiveInput`) providers, **never** fall through to `AddSteerMessage` — always deliver to the live session (or signal "session gone" so the server starts a new turn).
- Keep `AddSteerMessage`/`QueuedForInjection` **only** for non-live (API) providers.

### 3. Backend — `agent_go/cmd/server/polling.go` · `canSteerSession` (~32)
Simplify the CLI case to **tmux-session liveness** as the primary signal (it already falls back to `SessionHasBusyCodingTmux`). This is what the frontend `can_steer` reads, so it must agree with `handleSteerMessage` — both gate on the same liveness check to kill the race.

### 4. Frontend — `frontend/src/components/ChatInput.tsx`
- In `sendLiveCodingAgentMessage` (~1770): on a CLI session, **drop the steer→queue fallback + the toast**. `isLiveCodingSessionGoneStatus` (404/409/410) should no longer occur for CLI sends; if it somehow does, treat it as "start next turn", not "queue locally + toast".
- Remove (or repurpose) the **"The live coding-agent turn has ended. Sending this as the next turn."** toast (line ~1805) for CLI sessions.
- **Optional UX to preserve:** the pending-message list (`queuedMessages` + `handleSteerQueuedMessage`) can stay **purely as a display/cancel/edit affordance** — but it must NOT be a delivery gate. Default behavior is immediate send-keys.
- **Keep** the keyboard-passthrough fixes (space + coalescing, PR #75) — orthogonal.

## Preserve (do NOT remove)

- **BG-agent / orchestration path** — sub-agent completions are delivered into a busy session via the same steer mechanism (`[BG AGENT] Steered completion … into busy session`). It must ride the same unified "session alive → send-keys" path.
- **API/LLM agent steer-vs-queue** — still correct (no live channel; queue for next request).
- **The paste→settle→Enter reliability** (`sendInputToActiveTmux`, `waitForPromptPaste`, submit backoff) — orthogonal render-race handling; keep it.

## Edge cases / non-issues

- **Codex turn-boundary**: codex's `GenerateContent` returns after turn 1, so a follow-up lands as the *next* turn. Under this model that's correct — the message reaches codex and is processed; it's not lost.
- **Session genuinely gone** (CLI exited / retention expired): the only real fork — `startNextTurnFromSteer` opens a fresh session/turn.
- **Copy-mode can't eat input**: the pane is a detached, server-owned tmux session never attached by an interactive client, so user "scroll" is browser-side only. No copy-mode guard needed.
- **Multiple rapid messages**: send-keys each; the CLI queues them natively in order.

## Verification

1. The `…ProcessesQueuedFollowupContract` E2E tests (5 providers) stay green.
2. Manual: send a message to a live CLI session **between turns** (the old 404 window) → it must deliver with **no toast**, no lost message.
3. Confirm a normal send no longer emits the "live coding-agent turn has ended" toast.
4. Confirm BG-agent sub-agent completions still steer into busy sessions.
5. API-agent chats unchanged (steer-vs-queue still applies).

## Known adjacent bugs (separate tickets, not blockers)
- **agy** turn-completion stability race (declares done mid-`Generating…` before the first tool call). Fix lives in agy's stability/timing logic; "spinner = busy" is the wrong layer (breaks stale-status handling).
- **codex** wrapped-prompt extraction leak — **already fixed** on branch `codex-fix-and-queue-tests`.
