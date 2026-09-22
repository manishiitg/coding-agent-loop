[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-352 — Simplify chat render and restoration to one durable ordered log (design)

| Field | Value |
|---|---|
| Status | `design proposal; not implemented` |
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

## Migration (not a flag day)

1. Keep the current event envelope; add server-assigned sequence
   numbers to the in-memory window.
2. Persist the window on append (per-turn fsync batching).
3. Move readers over one surface at a time (chat, then work/crew/video,
   then bots/workflows), each switching to range reads + stable-ID
   dedup.
4. Delete the converters last, once no caller passes a second source.

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

Design ticket: verify by review, not tests. Acceptance is an approved
schema + endpoint shape (`GET log` ranges, sequence/ID semantics) that
the migration steps above can land against incrementally.
