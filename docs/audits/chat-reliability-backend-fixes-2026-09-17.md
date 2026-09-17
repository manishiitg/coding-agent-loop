# Chat reliability backend implementation — 2026-09-17

Implemented in the working tree, not deployed.

- Native transcript workers retain demand received during an active reconciliation window and run another full window. Missing, unreadable, and malformed initial history are retryable; known unsupported providers stop the window.
- Background recovery requires an identified owner and checks the durable transcript owner/session before importing native history. Unknown ownership is no longer assigned to `default`.
- Recovery demand is atomically persisted and fsynced under the private AgentWorks state root (`workflowCLIStateRoot()/chat-native-recovery`). A cancellable scanner replays markers at startup and every 30 seconds, with at most four concurrent reconciliations. Each marker includes owner, session, workspace, and request time. Markers remain explicitly unresolved because provider-native transcripts do not expose application submission IDs; no automatic resend or speculative delivery acknowledgement occurs.
- Full history writers, live-input append, title updates, migration destination writes, and native catch-up share path-striped mutexes across read/modify/write. Native catch-up re-reads canonical history after locking. Full-record writers merge already persisted rows and preserve structured tool entries and unknown metadata. `persistRawConversationSnapshot` provides the same boundary for legacy server/background writers.
- Every updated canonical transcript receives a monotonically increasing `revision`; projected builder restore responses also return it. Native write failure returns the previous canonical transcript rather than an unpersisted merge.
- Product continuation requests (`X-Conversation-Continuation: true`, or an existing verified candidate) reject replacement by another registry session. Established Work sessions must verify in the same project using saved or active workspace attribution. Fresh provisional browser session IDs remain supported.

Validation includes deterministic tests for demand arriving while recovery is active, retryable missing history, stale restore caller snapshots, recovered answer preservation during a later full save, structured raw entry preservation and revision increments, owner-isolated durable recovery markers, and fresh versus established continuation identity. Existing Claude/Codex/Pi/Muse native recovery tests now use canonical workspace fixtures because recovery re-reads before writing. Existing nondefault-owner native sync tests exercise owner and metadata index attribution.

## Remaining operational and architectural limits

These are single-server-process conversation locks. Multiple simultaneously writing server replicas require storage-level compare-and-swap or a transactional conversation store; blind workspace PUTs do not provide that guarantee. Deployments must preserve the configured AgentWorks state root, as they already must preserve native CLI session state.

Native repair still merges by ordered role/text occurrences. It cannot establish application turn identity or prove that a provider finished an ambiguous submission. Recovery markers remain unresolved and are retried periodically and on restart rather than being falsely marked complete. Windows are bounded and unresolved markers are retained; there is no automatic marker garbage collection yet.

An inability to persist a recovery marker is logged while in-process recovery still runs; completion observers cannot undo an already executed provider turn. The separate submission acceptance journal protects the earlier dispatch boundary.

## Follow-up: snapshot duplication regression

The first snapshot implementation incorrectly reused the native role/text merge with the incoming snapshot as its base. An older canonical tool-only row could match a newer incoming tool-only row (both projected to empty text). This moved the incoming recent human row ahead of the old canonical history and then appended its canonical copy later. Saving progressively extended snapshots duplicated prior turns.

A sanitized fail-before test (`TestSnapshotRetainedTailNeverPrecedesOlderCanonicalToolTurn`) demonstrated 10 rows instead of the expected 9 on the first save. A private production-data simulation using the original canonical prefix and three captured source snapshots reproduced one target human row growing from one occurrence to two, then three under the old merge. Canonical-first merging held it at one. No production content was added to tests or the repository.

Both application snapshot writers now use `mergeChatConversationSnapshots`, which retains canonical order and matches complete structured Role/Parts content rather than the lossy builder text projection; canonical top-level trace enrichment is preserved without creating a second message. It preserves tools and repeated occurrence counts, inserts canonical-only answers before new rows in the same gap, and keeps replayed snapshots idempotent. The native provider transcript merge is unchanged. This prevents further snapshot-induced duplication; it does not remove already persisted duplicate rows.
