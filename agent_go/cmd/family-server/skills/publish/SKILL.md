---
name: publish
description: Publish a family artifact (a progress report or the academic map) to a shareable destination — a shared folder, a static host, Google Drive, or GitHub Pages — tracking config vs status in files.
---

# Publish an artifact

Share a finished HTML artifact (usually a progress report or the academic map) so
another adult in the child's life — a co-parent, a tutor — can view it. Same
config vs status contract as AgentWorks:

- `publish.json` (at the workspace root) — declarative config: `enabled`, the
  destination, and which files it covers.
- `publish/status.json` — the operational result of the last publish (state,
  timestamps, published URLs/paths, errors).
Never write status into `publish.json`.

## Steps

1. **Read config.** `cat publish.json`. If it is missing, do NOT publish silently:
   tell the parent publishing isn't set up and ask where they'd like to share
   (a shared/synced folder, a static host, Google Drive, GitHub Pages). Setting up
   the destination is a one-time decision.

2. **First publish is ATTENDED.** Never do the first (verifying) publish
   unattended — confirm the destination with the parent so nothing goes to the
   wrong place. Only re-publish automatically once `publish/status.json` shows the
   destination is already `verified`.

3. **Publish** the covered files (e.g. `shared/reports/*.html`,
   `shared/academic-map.html`) using your shell. Easiest: **Google Drive via the
   `gws` CLI** — `gws drive +upload` the report, then share the link (the parent
   already has `gws` authenticated). Other options: copy to a synced folder, or
   push to a Pages branch, as configured. Never publish parent-only files
   (`parent/…`, answer keys) or secrets.

4. **Write `publish/status.json`** — `state` (`verified` / `pending` / `failed`),
   timestamps, the published location/URL per file, and any error.

5. **Tell the parent** what was published and where (the link/path), and who can now
   see it.
