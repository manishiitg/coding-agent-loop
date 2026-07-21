## Pulse finalizer

Use only after Gate and all due review/fixer modules. First read current Pulse
module state and confirm every due module for this Pulse run has a terminal
result. Never treat missing as skipped/successful. If anything is unresolved,
do not publish or notify a complete Pulse; report the incomplete state honestly.

Run these four commands in order in this one turn. Immediately before and after
each, call `mark_pulse_final_command_result` with the exact command name and a
truthful `running` then terminal status. Continue through Notify even when an
earlier command fails.

1. **Dashboard + questions.** Refresh `builder/card.health.html`, Today's outcome,
   compact technical detail, active assumptions, module outcomes, Bug/Goal
   freshness, user requests, backup/publish intent, and next action. When a real
   user/business decision is required, use `create_human_input_request`; never
   hand-edit request state or duplicate a matching request.
2. **Backup.** Load `backup-strategy`; perform Git/backup directly in this parent,
   never through a reviewer/sub-agent. Skip only when the exact current source
   hash is already backed up. Keep `backup/status.json` truthful. Set up the
   zero-config local-git default when backup is absent/disabled.
3. **Publish.** Read publish config/status. Skip when disabled, unverified, or
   already current. Never do the first/verifying publish unattended and never
   publish unbacked changes after backup failure. Keep status truthful.
4. **Notify.** Notify once every run. Account-level channels are inherited; lack
   of workflow Slack is not a reason to skip Gmail/email, and never write
   redundant account-wide Gmail config into `workflow.json`. Per-workflow
   preferences live in `workflow.json` `notifications` (`exclude_channels`
   suppresses an inherited account channel for this workflow; `block_recipients`
   is a per-workflow email denylist unioned with the account-wide one) and the
   backend applies them automatically — enforce a "do not email this workflow" or
   "never email address X" preference by setting `workflow.json` `notifications`,
   NOT by omitting the notification. Do not put notification preferences in
   soul.md. Include modules run/skipped, Bug/Goal state, requests, important
   outcomes, backup/publish, live URL, cost/time or next checkpoint, and next
   action. When protection is only local, prominently say
   `Backup risk: local only` until an off-device destination is verified.

Use rich email/Slack fields through `notify_user`; never read webhook secrets or
post directly. Keep user-facing language brief: takeaway first, labeled detail,
evidence last. Stop after recording terminal status for all four commands.
