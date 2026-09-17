# ChatTab / ChatInput / ChatArea isolation review

> Historical pre-fix review. See [implementation and validation](chat-reliability-implementation-2026-09-17.md) for the subsequent working-tree changes; the failing reproductions below describe the earlier checkout.

Date: 2026-09-17. Scope: multiple authenticated users, each with multiple application chat tabs. Application tabs and browser windows are different lifecycles. This is a local code review and focused logic verification, not a production or full browser reproduction. No application code was changed.

## Findings

### P1 — delayed queued messages lose their owning tab (reproduced in a logic harness)

`frontend/src/components/ChatArea.tsx:3370`, `:3440`, `:3457`.

The queue effect captures tab A, removes its messages, then waits 200 ms. It calls `submitQueryWithQueryRef.current` with `isAutoNotification` but without `sourceTabId`. That ref follows the current render. After selecting B, the callback can resolve B as its submission target. The timeout has no cleanup. The normal input route already supplies `sourceTabId`; the queue bypasses that safeguard.

Fix: capture an immutable submission envelope including owner/auth generation, tab, conversation, message ID and content; pass it through every delayed path. Selection is presentation state, never a delivery destination.

### P1 — rejected queued messages are removed permanently (reproduced in the same harness)

`frontend/src/components/ChatArea.tsx:3441`, `:3457`.

The queue is cleared before submission. Requeue happens only in `catch`, but the submit API returns `Promise<boolean>` and uses `false` for rejection. A resolved `false` therefore drops the queued human messages. Closing/reloading during the timeout also leaves no accepted durable submission to recover.

Fix: retain a pending queue record until positive acceptance; handle `false` explicitly and preserve stable message IDs across retry. This finding applies to the structured/non-interactive queue path; retained CLI input takes a different route.

### P1 — remounting the composer does not invalidate its asynchronous draft writes (code evidence)

`frontend/src/components/ChatArea.tsx:3898`; `frontend/src/components/ChatInput.tsx:1888`, `:2398`, `:2424`, `:2434`.

Keying ChatInput by tab correctly isolates component state, but an old instance's in-flight promise survives unmount. Its refs still identify A and its old submitted text. Example: submit from A; switch away; return to A and type a new draft in the new instance; old submission resolves. The old instance's equality guard can pass and `clearInputState` writes an empty draft into A's shared store. The analogous failed live-input branch can restore an old draft over newer text. Unmount preventing a local React state update does not prevent a Zustand write.

Fix: compare a store-owned draft revision/submission ID at commit time; clear or restore only the exact submitted revision. Component-local refs cannot validate edits made by a newer instance. The current helper tests model a changing ref in one instance, not this remount sequence.

### P1 — Work Builder redirection follows any selected conversation in the project (code evidence)

`frontend/src/products/work/workTabs.ts:69`; `frontend/src/components/ChatArea.tsx:2677`.

`resolveWorkSubmissionTab` redirects a Builder-origin send to the active non-Builder tab if it belongs to the same project. It does not establish that this is the conversation created by that Builder submission. An outstanding Builder send can therefore target an unrelated historical chat selected while it waits in the submission lane.

Fix: record the exact Builder-to-created-conversation binding for the submission sequence. Do not infer it from the currently selected tab. Existing tests explicitly cover same-project redirection but do not distinguish the newly created conversation from an unrelated one.

### P1 — account isolation relies on incomplete clearing rather than owner-scoped state (code evidence)

`frontend/src/stores/useChatStore.ts:3203`; `useWorkspaceConnectionStore.ts:83`; `useAuthStore.ts:114`, `:158`, `:181`, `:203`.

The persisted chat-store key includes workspace identity but not user identity. It retains tab config, including drafts and queues. Password login/logout call `discardChatStateForAccountChange`, but OAuth completion and current-user refresh do not. A changed identity through those paths can inherit stale tab/config state unless a separate navigation flow happened to clear it first. This is a shared-browser/account-transition risk, not proof of cross-user server leakage.

Furthermore, account reset clears current store data, observers and some timers, but does not invalidate outstanding hydration promises, component queue timeouts, or the module-level submission lane. `setTabEvents` accepts a session write without an account-generation check. A late old-account response can therefore repopulate memory after reset; any subsequent request also needs a stable identity check because API interceptors read the current token.

Fix: namespace durable state by workspace and authenticated user; centralize all identity transitions; increment an auth generation, cancel local pending work, and reject stale asynchronous commits. Keep server authorization independent and mandatory. Do not stop the previous user's server work merely because a browser logs out.

### P2 — background queue processing is tied to the selected tab (code evidence)

`frontend/src/components/ChatArea.tsx:3370`.

The queue effect observes `activeTab`, rather than all tabs with pending messages. When A completes after the user selects B, A's structured queued messages need not run until A is selected again, unless another mounted ChatArea happens to own A. Selection should not determine whether previously queued work progresses.

Fix: move queue processing to an account-scoped controller independent of the selected view.

### P2 — a view still owns global observer cleanup (code evidence)

`frontend/src/components/ChatArea.tsx:2592`.

Each ChatArea's cleanup calls `disconnectAllSSE()` on the shared store. It also runs when `pollingInterval` changes, not only on final unmount. Keying only ChatInput repaired tab-switch unmounts, but changing a parent surface or removing one of several ChatArea instances can still disconnect other sessions. Reconnection effects may repair this; the lifetime coupling remains.

Fix: one account/workspace-level session observer manager, with subscriptions owned independently of view components. Views release their own subscriptions, not every session's transport.

## Boundaries that already help

- AgentWorksChatTabItem passes the explicit tab ID for selection/close and the tab object for rename. It does not route messages itself.
- WorkflowChatTabs keys pills by tab ID.
- ChatArea keys ChatInput by target tab, while keeping the shared ChatArea mounted across ordinary tab switches.
- Normal ChatInput submissions pass a source tab ID; draft debounce cleanup flushes to the owning tab.
- The chat store keeps event buffers per session and rejects events explicitly labeled with a different session. Existing isolation tests verify interleaved streams.
- Account reset closes local observers without stopping the old user's server sessions.

These safeguards are worthwhile, but they must apply to every async path and account transition.

## Target responsibilities

1. **Tab:** immutable owner/project/conversation reference plus presentation state. Active selection never changes another submission's destination.
2. **Input:** draft and attachments owned by tab, with a revision. Submission snapshots those values once; completion conditionally consumes that revision.
3. **Conversation controller:** owns durable submission queue, acceptance, provider binding, event cursor and recovery for every open conversation. Independent of selected tab and React mounts.
4. **ChatArea:** renders a selected conversation and dispatches explicit actions; it does not own global queue/transport lifetimes.
5. **Authentication boundary:** namespaces state, invalidates old callbacks and verifies owner on every server action. Multiple browser windows additionally need identity-change handling and a policy for shared versus independent tab layouts.

This complements the durable turn/persistence review in `chat-reliability-architecture-review-2026-09-17.md`.

## Verification

42 existing tests passed across `useChatStore.sessionIsolation`, `useChatStore.hydration`, `chatSubmissionDraft`, `workTabs`, and `chatSubmitHelpers`.

`node docs/audits/chat-queue-isolation-reproducer.mjs` executes the current queue-effect body with controlled timers and submit callbacks. Both required-behavior assertions fail: tab A becomes B after selection changes, and `false` acceptance leaves an empty queue. This tests the actual effect logic in isolation, not a mounted React integration.

Required next integration cases: A→B inside the queue delay; rejected queue delivery; A→B→A with draft edits before old acknowledgement; Builder send followed by selecting an unrelated same-project chat; OAuth account change; late history after logout; two users with two chats each through backend restart; background queued message while another tab stays selected. Assert owner and conversation IDs as well as visible content. Cross-user server access remains a separate authorization test, not a conclusion from this frontend review.
