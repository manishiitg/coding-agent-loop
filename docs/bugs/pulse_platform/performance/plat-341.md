[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-341 — Native transcript recovery replay saturated the RTS agent CPU

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `deployed to RTS; focused regressions and service health green` |
| Last synchronized | `2026-09-21` |
| Priority | `P0 production performance / chat durability` |

## Report and production evidence

The operator reported that Ctrl+K and workflow switching on RTS had become
slow. Read-only inspection of `video.realtrainingsys.com` found the agent using
roughly 90% CPU over its lifetime and 147% CPU during a five-second sample on a
two-vCPU host. Memory and disk capacity were healthy.

A 15-second production CPU profile attributed 55.31% of all samples to
`replayPendingNativeTranscriptRecovery` and its reconciliation worker. Within
that path, `builderConversationMessageKey` accounted for 50.66% cumulative CPU,
`strings.Fields` for 33.80%, and garbage-collection work for 12.22%. RTS had 13
durable native-recovery demands, with the oldest dating to 2026-09-17.

The same inspection found no Video Studio-specific log rotation. At the time of
inspection, `agent.log` was approximately 821 MB and `workspace.log` was
approximately 1.8 GB. This log volume increased I/O and disk usage but was not
the primary CPU hotspot in pprof.

## Cause

Native transcript recovery is the durability fallback that repairs structured
AgentWorks chat history from a coding CLI's native transcript after a missed
completion, disconnect, or restart. The recovery feature is required; removing
it would reintroduce disappearing completed replies.

Two implementation details combined into the production regression:

1. Durable demands intentionally remained `unresolved` because native adapters
   do not expose the application turn ID needed for a strict delivery receipt.
   The fallback scanner consequently replayed every historical marker every 30
   seconds forever. Four recovery workers could perform expensive transcript
   reconciliation concurrently.
2. The LCS history merge normalized both messages inside every dynamic-
   programming matrix comparison. Normalization walks and allocates for all
   message text, changing the practical cost from O(n*m) comparisons to
   O(n*m*message-size) repeated string work. Long builder transcripts made a
   single replay expensive; perpetual concurrent replay kept the server hot.

Webhook bursts increased concurrent transcript activity and exposed the defect,
but they were not the underlying cause.

## Fix

- Precompute the normalized key of every persisted and native message once
  before the LCS pass. Matrix and reconstruction comparisons now use those
  cached keys. Equality checks use the same precomputed-key path.
- Persist attempt count, last-attempt time, and next-attempt time on durable
  recovery markers.
- Retry with bounded backoff: 1 minute, 5 minutes, 30 minutes, 2 hours, 6 hours,
  and 12 hours, with at most seven periodic attempts and a 24-hour maximum age.
  Legacy markers older than the age limit are marked exhausted without another
  transcript merge.
- Mark unsupported providers terminal instead of retrying them.
- Limit the low-priority historical recovery batch to one worker. The immediate
  post-completion reconciliation window remains unchanged.
- Prevent an older in-flight retry from overwriting a newer demand for the same
  owner/session/workspace identity.

The policy remains fail-safe for chat durability: newly completed turns still
receive the existing immediate bounded reconciliation window, and durable
fallback survives restarts. The change bounds only repeated historical work.

## Remaining architectural problem

The deployed fix bounds the production cost, but it does not make native
transcript recovery a simple or authoritative protocol. The recovery path still
has to coordinate all of the following concerns:

- an immediate post-completion retry window and a separate restart-safe
  periodic scanner;
- a filesystem journal plus process-local `pending` and `inFlight` state;
- provider-specific native transcript discovery and parsing;
- ownership, session and workspace validation before recovered content can be
  published;
- concurrent canonical-history writes and protection against an older attempt
  overwriting a newer recovery demand; and
- an ordered LCS merge based on normalized role and message text because the
  native adapters do not expose an application turn ID.

That last constraint is the fundamental source of fragility. Normalized text is
useful recovery evidence, but it is not a delivery identity. Legitimately
repeated messages, provider rewrites, partial streaming content and structured
tool rows all make text-based reconciliation heuristic. A durable demand
therefore cannot be marked `resolved` from an exact provider receipt; after the
bounded fix it eventually becomes `exhausted` instead. The implementation is
now operationally safe, but it remains a medium-to-high maintenance-risk
fallback and should not become another routine source of Formatted Chat truth.

## Long-term design

Formatted Chat should have one canonical ingestion path:

```text
provider/CLI adapter
  -> structured bridge event with stable owner, session and turn/message IDs
  -> append-only canonical AgentWorks chat history
  -> Formatted Chat projection
```

Tmux remains the required CLI process host and raw Terminal surface, but its
screen transcript does not feed the frontend directly. Provider adapters own
the conversion of native activity into structured events. The structured
bridge must persist an acceptance checkpoint and a completion checkpoint with
stable identities, so an interrupted or restarted server can determine exactly
which turn is missing without comparing whole conversations.

Native transcript recovery remains available only as an emergency repair path:

```text
missing structured completion checkpoint
  -> read the bounded native transcript tail for that provider turn
  -> normalize it once into structured events
  -> append idempotently using the stable turn/message IDs
  -> persist `resolved` and stop retrying
```

The recovery worker should consume explicit durable jobs rather than rescan a
directory of unresolved history markers. A job has a terminal state
(`resolved`, `unsupported`, `exhausted` or `corrupt`), an attempt lease, bounded
backoff and a recorded reason for every transition. Only one worker may own a
job at a time, including across processes. Reconciliation should operate on the
missing turn or bounded transcript tail, not perform an O(n*m) whole-history
merge during routine recovery.

The cutover is complete when:

1. every supported retained CLI emits stable acceptance and completion IDs;
2. Formatted Chat reads only canonical structured history;
3. a native recovery job can prove and persist `resolved` for an exact turn;
4. repeated user text and structured tool rows are recovered idempotently;
5. restart, disconnect and late-transcript-flush tests require no whole-history
   polling or frontend-native transcript merge; and
6. production telemetry distinguishes normal structured delivery from rare
   native repair, including queue depth, attempt count, terminal reason and
   repair duration.

Until those conditions hold, the bounded implementation remains necessary and
must not be removed: it protects completed replies from disappearing after a
missed completion, disconnect or restart.

## Verification and operational follow-up

- Focused native transcript, builder conversation, and recovery concurrency
  tests pass.
- Regression coverage verifies backoff, expiry, unsupported-provider terminal
  state, bounded worker concurrency, and preservation of newer demands.
- `git diff --check` passes.
- Deployed to RTS in release `6c47129-20260921082037`. All three services and
  the public HTTP endpoint passed health checks. The newly restarted agent was
  at 18.4% CPU in the initial process sample, versus the prior sustained
  saturation; a longer steady-state profile remains useful follow-up evidence.
- Rootless Video Studio now installs a low-priority user timer that checks logs
  every ten minutes, rotates at 100 MB with `copytruncate`, and retains seven
  compressed generations. Production-debug workspace messages are opt-in, shell
  diagnostics persist only command length plus a short SHA-256 fingerprint, and
  the unused agent `--log-file` flag is removed so systemd owns one clear log
  path. The registry-side hot-path suppression shipped in `mcpagent` commit
  `22ff53a` (included on `main` by merge `ee12433`).
- The first production rotation completed successfully during deployment. The
  active logs dropped to approximately 21 KB (agent) and 94 KB (workspace);
  the two original generations are retained intact for delayed compression.
