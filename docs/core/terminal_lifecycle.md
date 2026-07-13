# Terminal lifecycle

Runloop tracks a coding-agent terminal as two separate resources:

1. **Process lease**: ownership of the live tmux session.
2. **Terminal snapshot**: the read-only capture rendered in the UI.

The process lease is authoritative for cleanup. Hiding or expiring a snapshot
does not release a live process. Bounded workflow-step processes receive a
short shutdown grace after completion, while their snapshots can remain visible
for the longer provider display-retention period. Main-agent sessions are
persistent and use the idle backstop instead of the bounded deadline.

Every observed coding-agent tmux session is tagged with:

- `@runloop_instance_id`
- `@runloop_owner_pid`
- `@runloop_owner_started_at`
- `@runloop_heartbeat`

Startup recovery only removes tagged sessions whose owner PID/start-time pair
is no longer live. It preserves untagged legacy sessions and sessions owned by
another running backend or test process.

Terminal API state exposes:

- `process_state`: `live`, `closing`, or `closed`
- `snapshot_kind`: `live` or `archived`
- `close_reason`: the lifecycle reconciliation reason, when available

Operator actions follow these semantics:

- **Dismiss** hides only the snapshot.
- **Complete** asks the provider to finish and moves the lease toward closing.
- **Fail** closes the process and records a failed archived capture.
- **Kill** force-closes the process and records a failed archived capture.

SSE event buffers are retained for 30 minutes after a session reaches a terminal
state. Session identity remains available for 24 hours so chat history can be
resumed without retaining the full event stream.
