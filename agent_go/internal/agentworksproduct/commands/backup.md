{{context}}

Help me set up or run backup for this workflow. Call read_skill(skills=[{"name":"builder-reference","path":"references/backup-strategy.md"}]), then read workflow.json.backup and backup/status.json.
- If backup is NOT configured yet: recommend a private GitHub repository or another off-device destination first. Ask for the account/org, private visibility, and repository/bucket name before creating or connecting it. A local Git checkpoint is acceptable temporarily, but label it local-only and not durable; do not report it as a healthy backup.
- If backup IS configured: run a backup now and report the result (destinations, commit/ref).
- If I asked to restore: restore the tracked files from the latest backup (or a commit I name) instead.
Always write backup/status.json; never write operational status into workflow.json.
