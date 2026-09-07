# Schedule ownership and Builder warnings

The existing scheduler SQLite ledger owns workflow locks through its unique
active-run index. Builder execution and typed plan/config tools consult this
ledger at invocation, including background Builder agents. Inspection tools
remain available. An active run yields a `schedule_running` error before the
handler executes, with schedule ID, run ID, start time and lease expiry.

After the user approves concurrent work, the agent can retry the action with
`force: true`. This override is logged and does not release the schedule lock
or grant additional permissions. Approval is an agent instruction, not a
server-verified human confirmation token.

The server renews `updated_at` for the exact active run every 20 seconds;
expiry is that timestamp plus 90 seconds. Repeated renewal failure requests
run cancellation. Terminal transitions release ownership. Expiry alone never
removes an active database row, because a worker could still be performing
external actions. Existing startup reconciliation marks prior runs interrupted
before scheduling starts. This assumes one scheduler process and that the
service supervisor stops its child processes on restart (systemd's default
control-group kill behavior). It is not a distributed worker fencing system.

The guard checks the state at tool invocation. It does not lock arbitrary shell
writes or the entire Builder conversation, and does not serialize a long Builder
edit against a schedule that starts after the check. Raw planning writes remain
subject to the existing protected-file rules. UI REST edits are outside this
tool guard. These boundaries must not be described as a universal transaction
lock across all workflow activity.
