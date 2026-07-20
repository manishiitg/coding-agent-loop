---
name: backup
description: Back up the family's learning workspace to a durable destination (local git checkpoint, a private GitHub repo, or an object store like Cloudflare R2 / S3), tracking config vs status in files.
---

# Back up the workspace

Protect the family's data — materials, generated study/tests/reports, the academic
map, and conversations. Config and status are SEPARATE files (same contract as
AgentWorks):

- `backup.json` (at the workspace root) — the declarative config: `enabled`, `mode`,
  and `destinations`.
- `backup/status.json` — the operational result of the last attempt (state,
  timestamps, per-destination results, errors, and the current source hash).
Never write status fields into `backup.json`.

## Steps

1. **Read config.** `cat backup.json` if it exists. If it is missing or backup is
   disabled, do NOT silently skip: set up the zero-config **local git** default (no
   credentials needed) and back up. Then tell the parent that local-only is a
   rollback checkpoint, NOT durable off-device protection, and offer to add a remote
   (a private GitHub repo, or an object store like Cloudflare R2 / S3). Creating a
   repo/bucket is a one-time decision — ask the parent before creating one.

2. **Skip if unchanged.** Compute a source hash of the content, e.g.
   `find . -type f -not -path './.git/*' -not -path './backup/*' | sort | xargs shasum 2>/dev/null | shasum`.
   If `backup/status.json` shows this exact hash already backed up successfully,
   SKIP and report "already backed up".

3. **Back up** per the configured destinations, using your shell:
   - **local git (default):** `git init` (once), `git add -A`, `git commit -m "backup <date>"`. A local checkpoint only.
   - **private GitHub repo:** commit, then `git push` to the configured remote (repo + auth set up once — use `gh` if available, or the remote the parent configured).
   - **object store (Cloudflare R2 / S3):** `rclone sync` or `aws s3 sync` the workspace to the configured bucket (bucket + credentials set up once).
   - **large binaries:** a HuggingFace dataset repo if configured.
   - NEVER back up secrets (the parent PIN hash lives outside the workspace — keep it that way; do not copy it in).

4. **Write `backup/status.json`** — `state` (`healthy` for a verified remote, `local_only`, or `failed`), last attempt + success timestamps, per-destination result, any error, and the current source hash.

5. **Tell the parent** in plain words: what was backed up, where, and whether it is durable (remote) or just a local checkpoint. If no remote is configured, offer to set one up.
