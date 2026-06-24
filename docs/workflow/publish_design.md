# Publish — share a workflow's HTML to a public URL

Status: **Design** (2026-06-24). Not implemented.

## Goal

A workflow already produces HTML artifacts — the **Pulse log** (`builder/improve.html`)
and the **reporting dashboard** (`reports/`). We want to **publish them to a public URL**
on any static host (Netlify, Vercel, Cloudflare Pages, GitHub Pages, S3, a VPS, …), driven
by the **builder agent** — exactly the way Backup is agent-driven.

## Publish = Backup's twin

Same shape, pointed at public hosting instead of durable storage. The Backup scaffold
(`workflow_backup.go`, the Backup popup, the toolbar status dot, `backup-strategy.md`) is
the template; Publish mirrors each piece.

| | **Backup** (exists) | **Publish** (new) |
|---|---|---|
| Goal | "can I get it back?" — durable storage | "can I share it?" — a public URL |
| Config | `workflow.json.backup` | `workflow.json.publish` |
| Status | `backup/status.json` | `publish/status.json` (incl. the live **URL**) |
| Endpoint | `handleRunWorkflowBackup` | `handleRunWorkflowPublish` (mirror it) |
| Reference doc | `backup-strategy.md` | **`publish-strategy.md`** |
| UI | Backup popup + toolbar dot | **Publish popup + toolbar button (shows URL + status dot)** |
| Auth | creds / secret | a provider token via the secrets system |
| Agent prompt | `buildWorkflowBackupAgentPrompt` | `buildWorkflowPublishAgentPrompt` |

The builder reads `workflow.json.publish` + `get_reference_doc(kind="publish-strategy")`,
deploys the HTML, and writes `publish/status.json` with the public URL.

## Provider-agnostic by construction (agentic, no per-provider Go)

The whole point: **we do not enumerate providers in Go.** Adding a host is a doc edit, not
a release — same philosophy as Backup (whose `backup-strategy.md` covers "any large-file
backend" via a matrix + `rclone` as the catch-all, with zero per-backend Go).

- Go stays generic: `workflow.json.publish` carries a **free-form `provider` string**, the
  deploy details, an auth secret name, and the resulting URL. Go never knows what "Netlify"
  is.
- `publish-strategy.md` holds the knowledge — a **general static-deploy method** + specifics
  for common hosts.
- The agent has shell + the user's token, so it runs whatever the host needs and reads back
  the URL.

### The "any static host" method — three universal paths
The doc gives the agent these, and it picks whichever the named provider supports:
1. **Provider CLI** — `netlify deploy`, `vercel`, `wrangler pages deploy`, `gh-pages`,
   `surge`, `firebase deploy`, …
2. **Git-push-to-deploy** — push the static files to a repo the host auto-builds
   (Netlify / Vercel / Pages / Render). Reuses the git skill Backup already uses.
3. **Object-store / file sync** — `aws s3 sync` / `rclone` / `rsync` to a static bucket or a
   server+nginx. The true catch-all: any host that serves files from a bucket or directory.

Between these, "any provider with static hosting" is covered with no provider-specific code.
New host you've never configured? Set it up conversationally with the builder
("publish to my Cloudflare Pages project X") → it writes the config and deploys.

## Publishing the dashboard (the wrinkle)

The two artifacts differ:
- **Pulse log** (`improve.html`) — self-contained HTML. Publishes as-is. ✅
- **Reporting dashboard** (`reports/`) — **live**: it calls `window.report.query(sql)`
  against `db/db.sqlite` *inside the app*. Static hosting has neither the bridge nor the DB,
  so it would render empty.

**Decided (2026-06-24): Option A — static HTML generation (snapshot).** The agent bakes the
dashboard to static HTML at publish time; no live DB, no client-side DB. B and C below are
recorded for context but are **not in scope** (sql.js may be a future opt-in if interactivity
is ever needed).

- **A. Snapshot / bake → static HTML (the approach).** At publish time the agent runs every
  query the dashboard uses, inlines the results as JSON, and injects a shim so the page reads
  baked data:
  ```js
  window.__REPORT_DATA__ = { "<normalized sql>": [...rows] }
  window.report = { query: (sql) => window.__REPORT_DATA__[normalize(sql)] ?? [] }
  ```
  Fully static, works on any host. Snapshot-as-of-publish — **auto-republish after each run**
  keeps it fresh. Also the **privacy-safest**: only the baked query results are exposed, not
  the whole DB. Handles the common case (a fixed set of queries).
- **B. sql.js in the browser** *(out of scope — future opt-in).* Ship `db.sqlite` + sql.js
  (SQLite→WASM) for a genuinely interactive dashboard, at the cost of exposing the whole DB.
- **C. Serverless function** *(out of scope).* A Netlify/Vercel function queries the DB to
  keep data server-side; not pure static, most setup.

The snapshot is an agentic transform the agent performs at publish time per
`publish-strategy.md` — read the report's queries, run them against `db.sqlite`, inline the
results + shim, deploy the static bundle.

## Data privacy (must be explicit in the flow)

**Publishing a dashboard publishes its data.** Snapshot or sql.js, whatever's in the queries/
DB goes to a public URL. So:
- `workflow.json.publish` should let you **scope what's published** (which artifacts; for the
  dashboard, which views/queries, or a sanitized/filtered DB).
- `publish-strategy.md` makes the agent **warn before exposing raw rows** and confirm the
  scope, especially for sql.js (whole-DB) publishes.

## Config shape — `workflow.json.publish`

Generic and open-ended (no provider enum):

```json
{
  "enabled": true,
  "mode": "agent",
  "targets": ["pulse", "report"],          // which HTML artifacts to publish
  "dashboard_mode": "snapshot",            // snapshot (static HTML) — the decided approach
  "destinations": [
    {
      "id": "main-site",
      "provider": "<free string: netlify|vercel|cloudflare-pages|github-pages|s3|...>",
      "method": "cli|git|sync",            // which universal path
      "site": "<project/site/bucket/repo identifier>",
      "secret_name": "<global secret holding the token>",
      "public_base_url": "<filled in by the agent after first deploy>"
    }
  ],
  "triggers": ["manual", "post_run"]
}
```

## Status — `publish/status.json`

```json
{
  "version": 1,
  "state": "not_configured | configured_not_verified | publishing | published | stale | failed",
  "url": "<public URL of the latest publish>",
  "last_published_at": "<ISO>",
  "last_attempt_at": "<ISO>",
  "last_source_hash": "<so 'stale' = artifacts changed since last publish>",
  "destinations": [ { "id": "...", "provider": "...", "url": "...", "state": "...", "error": "" } ],
  "last_error": "",
  "updated_at": "<ISO>"
}
```

`stale` reuses Backup's source-hash trick: the artifacts changed since the last publish, so
the public URL is behind.

## Backend (mirror `workflow_backup.go`)

- `handleRunWorkflowPublish` — accepts `action: "publish" | "configure"` (+ later `unpublish`).
  Spawns a builder session with the publish prompt; writes `publish/status.json`.
- `buildWorkflowPublishAgentPrompt` — the agentic contract: read `workflow.json.publish` +
  `get_reference_doc(kind="publish-strategy")`; for each destination, prepare the static
  artifacts (bake the dashboard per `dashboard_mode`), deploy via the destination's method,
  capture the URL, write status. Confirm scope before exposing data.
- Route under `/workflow/publish`, `/workflow/publish/run`, `/workflow/publish/config`.
- `publish-strategy.md` registered in `guidance.go` (Modes: workshop/run — it's a workflow
  builder capability, like backup-strategy).

## UI (mirror the Backup popup + toolbar)

- **`WorkflowPublishPopup.tsx`** — public URL (with copy/open), per-destination status,
  "Publish now" / "Set up", scope (which artifacts), the configured destinations, and an
  illustrative "common hosts" hint list (suggestions, not a gate). Reuse `backupStatus.ts`
  patterns for the state→visual mapping.
- **Toolbar button** — a "Publish" button (Globe/Upload icon) with a status dot
  (`published` green, `stale` amber, `failed` red, `not_configured` grey) and the URL in the
  tooltip. Sits next to the Backup button.
- AI-driven only (no manual config editor), consistent with Backup.

## Setup & verify first (same as Backup)

Publishing follows Backup's set-up-then-verify flow — the user configures and **tests it
manually before anything runs unattended**:

1. **Configure** (`action: "configure"`) — the builder sets up `workflow.json.publish` with
   the provider/destination/token. State → `configured_not_verified`. Nothing is public yet.
2. **Test publish** (manual "Publish now") — the first real deploy. The agent publishes, gets
   a working URL back, and confirms it loads. State → `published` (verified).
3. **Then auto-republish is allowed.** Until a destination has had at least one successful
   (verified) publish, the post-run trigger does **not** fire for it — so we never push an
   unproven config to a public URL unattended. This matches Backup's
   `configured_not_verified` → `healthy` gate.

## Triggers (decided 2026-06-24)

Publish rides the **same cron-driven post-run Pulse pass as Backup** — it's one more step in
the post-run steward, not a separate hook:

> back up → triage → fix → **publish** → notify

- **Auto (cron):** on a scheduled run, if publish is **on** (and verified, and artifacts
  changed), the Pulse pass publishes — so the public dashboard/Pulse log stay current with no
  manual action. Gated three ways: publish **on**, destination **verified** (a prior
  successful manual publish), and **source-hash changed** (skip an unchanged artifact set).
- **Manual:** if publish is **off** (or for an unscheduled workflow), it's a manual "Publish
  now" via the builder — exactly like Backup's manual run. The manual publish is also the
  verification step above.

So: **publish on + cron → auto-publish via Pulse; otherwise → manual via the builder**, the
same on/off-then-manual shape Backup has.

## Org Pulse publishes too (same mechanism)

Publish isn't workflow-only. The **Chief of Staff's Org Pulse** produces its own HTML
(`pulse/org-pulse.html` — the org heartbeat log), and it can be published the same way so the
whole org's status is shareable at a public URL.

- Same **provider-agnostic agentic mechanism** — one `publish-strategy.md` serves both; only
  the **source HTML** and **config location** differ.
- **Workflow publish:** config in `workflow.json.publish`; targets `improve.html` + `reports/`;
  runs in the workflow Pulse post-run pass.
- **Org publish:** config in the CoS config (`multiagent-config.json`); target
  `pulse/org-pulse.html`; runs as a step in the **Org Pulse** daily pass (`org-pulse.md`),
  same on/verified/changed gating.
- Same setup→verify→auto-via-cron flow on both.

So the Publish build should treat the "what HTML + where's the config" as parameters, not bake
in "workflow." Reference doc and deploy logic are shared; the two Pulse passes each call it
with their own artifact + config.

## Build plan

1. **Backend scaffold** — ✅ Done (2026-06-24). `WorkflowPublishConfig`/`WorkflowPublishDestination`
   in `workflow_manifest.go`; `workflow_publish.go` (status types, `publish/status.json`,
   source-hash over improve.html + reports/ + db.sqlite, `handleGetWorkflowPublish` /
   `handleUpdateWorkflowPublishConfig` / `handleRunWorkflowPublish` with `configure`/`publish`
   actions, `buildWorkflowPublishAgentPrompt`); routes `/workflow/publish[/config|/run]`.
   Mirrors `workflow_backup.go`. Build + vet clean. **Manual publish path works end-to-end.**
2. **`publish-strategy.md`** — ✅ Done (2026-06-24). The agentic playbook, registered as
   `get_reference_doc(kind="publish-strategy")` (workshop/run/multi-agent). 3 universal deploy
   paths + static-snapshot procedure + privacy/scope + setup→verify + status contract. Serves
   both workflow and org publish.
3. **UI** — ✅ Done (2026-06-24). `publishStatus.ts` (state→visual + dot), `WorkflowPublishPopup.tsx`
   (state header, prominent **public URL** with open/copy, Publish now/Set up, destinations,
   common-hosts hint), toolbar **Publish** button (Globe + status dot) in `WorkflowToolbar.tsx`,
   API methods + types. tsc clean.
4. **Auto-republish via Pulse** — ✅ Done (2026-06-24). Added **step 4 PUBLISH** to the
   post-run Pulse pass (`scheduler.go` `runPostRunMonitor` is now 5 steps:
   back up → triage → fix → **publish** → notify) and a matching **4b** section in
   `post-run-monitor.md`: re-publish only an already-**verified** destination, only when
   artifacts changed; never the first unattended publish.
5. **Org publish** — ✅ Done (2026-06-24). Added **step 6** to `org-pulse.md`: publish
   `pulse/org-pulse.html` per `publish-strategy.md`, same verified+changed gate, opt-in via a
   CoS `publish` config / `pulse/publish.json`. Fully agentic (no new Go) — the Org Pulse
   agent deploys via shell per the shared reference doc.

## Decided (all 2026-06-24)
- **Dashboard:** **Option A — static HTML generation (snapshot)**. The agent bakes query
  results into static HTML at publish time. sql.js/serverless out of scope.
- **Provider model:** provider-agnostic + agentic — no per-provider Go; `publish-strategy.md`
  holds the 3 universal deploy paths.
- **Trigger:** **manual + auto-republish**, both from v1. Auto-republish is a **step in the
  cron-driven Pulse post-run pass** (back up → triage → fix → publish → notify), gated on
  publish-on + verified + source-hash-changed. Off → manual via the builder, like Backup.
- **Setup/verify:** configure → `configured_not_verified` → manual test publish → `published`;
  auto only after verified (same as Backup).
- **URL:** **host's default URL** (e.g. `*.netlify.app`, `*.pages.dev`) for v1; custom
  domains deferred.

*(No open questions — design is settled; ready to build.)*

## Non-goals
- No per-provider Go adapters — providers live in the reference doc.
- No manual JSON config editor (AI-driven, like Backup).
- Not a general web host / CMS — only deploying the workflow's existing HTML artifacts.
