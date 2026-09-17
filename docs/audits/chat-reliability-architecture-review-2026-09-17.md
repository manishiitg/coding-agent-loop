# PLAT-324 architecture review — 2026-09-17

> Historical pre-fix review. See [implementation and validation](chat-reliability-implementation-2026-09-17.md) for the subsequent working-tree changes; the failing reproductions below describe the earlier checkout.

Reviewed local checkout at `8a4e74f76`, including existing working-tree changes. No application code or existing changes were modified. No production messages, deployments, or production reproduction were performed.

## Assessment

There is a shared architectural problem behind the repeated incidents: delivery, durable conversation history, provider-native history, live events, and browser restoration advance separately, without a single durable turn identity and revision governing their transitions. The existing fixes address real defects, but correctness still depends on timing, process-local tracking, and later best-effort repair.

Two promises must be tested separately: (1) the UI retains the transcript; (2) the next provider invocation receives the same conversation context. Passing the first does not prove the second. Neither does a healthy deployment.

## Findings

### 1. P1 — older hydration can still remove a newer durable answer (reproduced)

`frontend/src/utils/sessionRestore.ts:263`, `:290`, `:367`.

The latest fix preserves raw live events with stable IDs, but drops every synthetic `restored-*` carrier before replacing the transcript. There is no per-session request generation or durable snapshot revision check.

Reproduction: start restore A; start restore B; resolve B with a human message and assistant answer; resolve A with only the earlier human message. B displays the answer, then A removes it. Both live windows are empty, as can happen after restart. The new regression test fails on its final assertion. Existing restore tests all pass.

This is the saved-history variant of the flash/disappear bug. Preserve monotonic durable revisions; reject stale commits at the shared store boundary. A request-generation guard is useful immediate containment, but cannot detect an older server snapshot returned to a newer request. Intentional deletion needs explicit versioned semantics.

### 2. P1 — CLI delivery succeeds independently of durable acceptance (code evidence)

`agent_go/cmd/server/server.go:9183`, `:9295`; `chat_history_persistence.go:3605`, `:3635`.

The warm live-input path sends to the provider, writes the successful HTTP response, and then invokes a persistence function returning no error. The other path also sends before persistence. Missing/unreadable history and path lookup failures silently return; write failures only log. No transcript is created here before the first completed turn.

Consequently, “delivered” does not guarantee that the accepted message survives restart in application history. Returning an error after sending is insufficient and can invite duplicate retries. Persist a submission ID and pending state before dispatch, record provider acknowledgement separately, and make retry use the same ID. Ambiguous provider delivery must stay explicitly ambiguous until reconciled.

### 3. P1 — recovery can miss a later turn because coalescing drops its request (code evidence)

`agent_go/cmd/server/claude_native_transcript_sync.go:87`, `:106`; `server.go:8162`.

An in-flight sync causes subsequent completion-triggered sync calls to return without a dirty flag or deadline extension. The first worker reads at approximately 0, 0.3, 1.3, 3.3, 7.3, and 15.3 seconds, plus I/O time. A second completion near the end of that window gets no independent recovery window. If its native final flushes after the last read, no further read is scheduled by this worker.

This is stronger than merely choosing too short a timeout. Coalescing loses work. Also, a missing/unreadable initial application transcript returns `supported=false`, ending retries even though a transient failure is not an unsupported provider.

Track recovery demand per turn or at least a dirty generation, distinguish retryable failures, and persist pending recovery across restart. Recovery should end when the expected turn is durably accounted for or explicitly unresolved, rather than when a timer expires.

### 4. P1 — independent whole-file writers can overwrite each other's progress (code evidence)

`chat_history_persistence.go:3621`; `claude_native_transcript_sync.go:490`, `:583`; `workflow.go:4695`.

Live-input append and native transcript catch-up both read a conversation snapshot, change it, then PUT the whole JSON record. Neither read-modify-write sequence shares a conversation lock or compares an expected revision. The native sync mutex only coalesces native workers; history index locks protect the index, not these transcript updates.

A live append can read H while reconciliation reads H; reconciliation writes H+answer; the append writes H+user and removes the answer, or the opposite order removes the user. Serializing individual file PUTs does not prevent this stale-read overwrite. Native history may repair it later, but that makes durability depend on the recovery window above.

Use a single conversation writer or atomic append/compare-and-swap with retry. Prefer durable message records over competing whole-transcript rewrites.

### 5. P1 — failed Work session verification does not enforce the stated fail-closed contract (code evidence)

`agent_go/cmd/server/agent_profile_routes.go:676`, `:695`, `:706`, `:548`; `product_conversation_registry.go:377`.

The Work repair only switches the registry when the supplied session verifies in the project. If verification fails and the registry already identifies another accessible session, repair is skipped and that registry record is returned. The handler then replaces `X-Session-ID` with that session. Ownership validation of the replacement is not proof that it is the conversation shown in the tab.

Thus a missing indexed history entry can still route an established tab to a different conversation rather than visibly failing. Distinguish explicit new-chat creation from continuation. Continuation must either resolve exactly the expected durable conversation or return a conflict/recovery error; failed verification must not silently select another session. This is a code-path finding, not a production reproduction.

## Recommended repair order

1. Contain known failures: reject stale hydration commits; reject unresolved continuation identity; retain recovery demand arriving during an in-flight sync; serialize transcript updates. Add adversarial tests for each.
2. Establish one durable application conversation/turn store. Scope it by owner and project; persist immutable conversation, turn, submission, and message IDs. Provider session IDs are bindings to that conversation, and browser tabs are references to it.
3. Persist accepted input before provider dispatch. Track accepted, delivery-confirmed/uncertain, running, provider-finished, and transcript-persisted states explicitly. Persist queued turns and recovery cursors; process-local maps are caches.
4. Commit messages before publishing their durable events. UI snapshots include a revision and streaming resumes from that revision; raw transport progress can remain ephemeral. Native transcript import uses provider message IDs/offsets, with explicit adapter guarantees when those are unavailable. Text-occurrence matching is a repair heuristic, not a reliable message identity.
5. Gate release confidence on failure tests: restart at acceptance/dispatch/completion/persistence boundaries; reconnect with old responses in flight; send two messages while busy; delay native flush beyond the old retry window; fail transcript writes; remove a history index entry; switch tabs; resume and ask a context-dependent follow-up on each supported CLI. Assert both visible history and actual provider continuity.

This can be implemented incrementally behind the current API. A UI rewrite or simply increasing retry delays does not establish these guarantees.

## Validation and limits

Ran the existing `sessionRestore.test.ts` and `sessionRestore.productFallback.test.ts` alongside one new deterministic reproduction: **19 existing tests passed; the additional stale durable snapshot test failed**.

The exact reproducer is saved as `docs/audits/plat-324-stale-hydration-reproducer.test.ts.txt`, outside test discovery. To rerun, copy it to `frontend/src/utils/sessionRestore.audit.test.ts` and run `npm --prefix frontend test -- src/utils/sessionRestore.audit.test.ts`; remove that temporary copy afterward. It intentionally asserts the required behavior and currently fails.

Backend findings are based on the inspected call paths; no new backend fault-injection tests or full server suite were run. The existing ticket itself says the multi-tab context-dependent deployment follow-up matrix remains outstanding despite its overall live-verified status. The evidence is sufficient to identify architectural gaps, but does not identify which one caused a new production incident without its request/session evidence.
