[← Pulse platform index](../../pulse_platform_issue_register.md)

# PLAT-341 — Native transcript recovery replay saturated the RTS agent CPU

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | `implemented; focused regression tests green; deployment pending` |
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

## Verification and operational follow-up

- Focused native transcript, builder conversation, and recovery concurrency
  tests pass.
- Regression coverage verifies backoff, expiry, unsupported-provider terminal
  state, bounded worker concurrency, and preservation of newer demands.
- `git diff --check` passes.
- Deployment and a post-deployment RTS CPU profile remain pending.
- Add rootless Video Studio log rotation separately (`copytruncate`, compressed
  retention) and retain a diagnostic tail before reclaiming the current logs.
  Log truncation is operational cleanup, not the CPU fix, and was not performed
  during this investigation.
