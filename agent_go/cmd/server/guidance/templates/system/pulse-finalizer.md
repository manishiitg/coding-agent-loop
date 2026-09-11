## Pulse finalizer

Use only after Gate and every due module.
Confirm every due module has a terminal result. Never treat missing as skipped/successful.
Pulse state, questions, findings, fixes, and history live in SQLite and are
shown in the Pulse popup. Do not write a separate presentation artifact in this turn.

Run Backup, Publish, then Notify. Before and after each, call
`record_pulse_result` with `command` set to its exact name and a truthful
`running` then terminal `result`. Continue through Notify after individual failures.

1. **Backup.** Load `backup-strategy`; perform backup directly in this parent,
   never through a reviewer/sub-agent. Skip only when the current source hash is
   backed up. Keep `backup/status.json` truthful and use the zero-config local-git default
   when backup is absent. When a destination covers `db-sqlite`, use the
   backend-created `backup/database/db.sqlite` snapshot and
   `backup/database/db.sqlite.sha256` checksum supplied in the
   finalizer context, or call `create_workflow_database_snapshot` if no current
   snapshot was supplied. Stage both managed files, never protected live
   `db/db.sqlite` or its WAL/SHM files.
2. **Publish.** Publish is independent of Backup: a partial or failed backup
   must not suppress an otherwise valid publish attempt. Skip only when publish
   is disabled, its artifact is unverified, it is already current, or the
   publish operation itself fails. Never perform first verification unattended.
   Keep status truthful and record the live URL.
3. **Notify.** Notify every run. Account channels are inherited; absent workflow
   Slack never suppresses Gmail. The backend applies `notifications`
   exclusions/recipient blocks. Never copy account config into `workflow.json`,
   put notification preferences in soul.md, or skip sending to enforce one.

   Read current worklist/module results, saved reviews, finding
   lifecycle/dispositions, and pending human requests from SQLite-backed tools;
   dashboard prose is not notification truth. A module result of `changed` is
   not stronger behavioral proof, but an applied repair closes its issue.
   `fixed_verified`, `verified_no_change`, and `changed_unverified` are complete;
   a failed repair and every active/blocked/awaiting-user finding remains pending.

   By default send one notification with distinct **Run outcome** and **What
   Pulse did** parts. Run outcome covers execution outputs, failures, goal
   movement, and metrics. What Pulse did is this compact review-and-fix digest:

   - **Pulse verdict:** Bug state, Goal state, and takeaway.
   - **Reviews completed:** modules reviewed and plain-language conclusions;
     distinguish clean from incomplete.
   - **Issues found this pass:** each material new/reopened issue, severity,
     impact, and review area. Say “none” when proven; incomplete is not clean.
   - **Fixed by Pulse:** Fixer changes and verification. Separate verified
     fixes/no-change closures and note when an applied fix is being monitored
     for recurrence.
   - **Still pending:** exact active count, highest-priority current and retained
     issues, blocker, next owner, and checkpoint. If over five, show the top five,
     remaining count, and tell the user to open Pulse for details.
   - **Needs your decision:** pending requests and what each unblocks.
   - **Operations:** backup/publish, the backend-supplied current-pass
     **Reviewers + Fixer** cost, time, and next Pulse action. Preserve the
     supplied cost label exactly. It covers only the Review+Fix parent turn,
     its background reviewer/fixer agents, and any receipt continuation. Never
     substitute Gate, Finalize, cumulative daily, workflow-execution, builder,
     or prior-pass cost.

   If a newly found issue remains pending, say so instead of duplicating it
   ambiguously. Say `Backup risk: local only` until an off-device destination is verified.

   Apply **WORKFLOW RUN SUMMARY INSTRUCTIONS** only to Run outcome and **PULSE
   REVIEW SUMMARY INSTRUCTIONS** only to What Pulse did. They cannot change
   recipients, channels, secrets, permissions, or safety.

   With **SPLIT NOTIFICATION ROUTING**, send
   `notify_user(notification_kind="run_summary")` with only Run outcome, then
   `notify_user(notification_kind="pulse_summary")` with only What Pulse did.
   Both must succeed; report partial failure without duplicating sections.

Use the channel-neutral `summary_title`, `summary_status`, `summary_fields`,
and `summary_sections` fields on every run or Pulse summary. `summary_status`
must say what the workflow is doing now: `completed`, `failed`, `blocked`,
`waiting_for_user`, `waiting_for_platform`, `monitoring`, `informational`, or
`no_run`. Explain the cause and any needed move in the title, message, facts,
or sections; do not invent separate lifecycle, owner, or next-action fields.
The Org Dashboard stores those fields as durable workflow history, while Gmail,
Slack, and WhatsApp receive the same notification through their configured
renderers.
Treat major `routing` choices as sub-workflows (PLAT-259); `branch` choices
remain inside their route. Use `summary_routes` for route-specific Run and
Pulse facts, even when only one route is covered. Each entry names the exact
`routing_step_id` and `route_id`, a readable label, title, status, and message;
optional fields/sections hold its evidence and next action. Read actual run
selection/output receipts for Run outcome and persisted route-scoped focus
history/findings for Pulse. Scheduled route selections alone do not establish
what Pulse reviewed. Do not label unreviewed routes clean, or routes that did
not run failed. Mention meaningful uncovered scope honestly.

Keep one digest per existing notification kind/routing policy with one entry
per covered route. Keep shared work and workflow-wide operations in the
top-level message/sections. Do not allocate total workflow or Reviewers + Fixer
cost to individual routes without route-attributed ledger evidence. Put route
actions, verified outputs, blockers, and evidence boundaries in their own
entries; one route succeeding must not imply another is healthy. The overall
status must acknowledge material route blockers rather than hiding them.
The backend renders these entries into channel messages: do not duplicate
their bodies in `message_for_user`, Slack sections, or email HTML. This changes
content grouping, not recipients or notification frequency.
Legacy `summary_route` remains readable but cannot identify same-named routes
under different routing steps. Do not combine it with `summary_routes` or guess
missing historical scope from prose. A shared Pulse review has no route entry.
Channel-specific rich fields may add presentation detail, but must not carry
facts that are missing from the neutral summary. Never read webhook secrets or
post directly.
For Gmail, use compact inline-styled `email_html` with readable status chips and
issue/fix cards for the sections above, not generic prose. Put takeaway first
and evidence last. Stop after all three terminal statuses.

Use ordinary language. Do not expose manifests, finding IDs, hashes, packet
names, paths, or state codes in notifications. Keep them in SQLite-backed
records and the Agent log; include one diagnostic reference only when required
to explain a failure.

## Lead the Pulse summary with goal progress
Before composing pulse_summary (including split-routing Pulse email), call get_goal_metrics. Use the tool's deterministic progress snapshots (value, change_from_previous, state and observed_at) rather than inventing a score or recalculating across unlike series. The backend automatically inserts Goal progress and Supporting metrics into both run_summary and pulse_summary, including custom email HTML; do not duplicate those blocks in email_html/email_html_file. Use these facts in the interpretation: primary metric latest valid value + unit, measurement window and observation date, target if configured, and comparable change. Add compact supporting metric facts next, then decisions and review actions. Missing/stale data must be labelled, never replaced with zero or an evaluation score. If no metrics are configured say "Measurement setup needed — /setup-goals". Do not claim a goal was reached from more tasks/posts/reviews alone. Respect explicit summary preferences; keep recipients/routing unchanged. Do not re-run collectors or ask questions in the finalizer.
