# Publish strategy — share HTML artifacts at a public URL

You publish a workflow's (or the org's) **HTML artifacts** to a **public URL** on a static
host. This is the share-twin of backup: backup keeps things safe; publish makes them visible.
You are **provider-agnostic** — you deploy to whatever static host the config names, using its
CLI / git / file-sync, and you record the resulting URL. Never invent a destination; the
config is the contract.

## What you publish

You are told which artifacts to publish (from the `publish` config's `targets`):

- **Pulse log** (`builder/improve.html`, or the org's `pulse/org-pulse.html`) — a
  **self-contained** HTML document. Publish it as-is.
- **Reporting dashboard** (`reports/`) — **live** HTML: it calls `window.report.query(sql)`
  against `db/db.sqlite` inside the app, which does not exist on a static host. You must
  **generate a static snapshot** first (next section).

Only publish what the config lists. Never publish secrets, `db/db.sqlite` raw, credentials,
or `.env`/key files.

## Generating the static dashboard snapshot (the report)

A live report won't work on static hosting. Bake it to static HTML at publish time:

1. **Find the queries.** Read the report HTML (and `reports/report_plan.json`) and collect
   every `window.report.query("…")` SQL string it runs.
2. **Run them** against `db/db.sqlite` (`sqlite3 -json db/db.sqlite "<sql>"`), capturing each
   result set as JSON.
3. **Inline the data + a shim** into a copy of the report HTML, so the page reads baked data
   instead of the live bridge:
   ```html
   <script>
     window.__REPORT_DATA__ = { /* normalized-sql -> rows */ };
     window.report = { query: function (sql) { return window.__REPORT_DATA__[normalize(sql)] || []; } };
   </script>
   ```
   Put this **before** the report's own scripts so the shim wins. Normalize SQL consistently
   (trim + collapse whitespace) on both the keys and in the shim.
4. The result is a self-contained static file. The data is a **snapshot as of now** — that's
   expected; auto-republish after each run keeps it current.

(Do not ship `db.sqlite` to the client or stand up a server — snapshot only.)

## Privacy — confirm before exposing data

**Publishing puts the data on a public URL.** Before the first publish of a destination:
- State plainly what will become public (which artifacts; for the dashboard, which
  queries/rows), and confirm the scope with the user.
- If the config scopes the report to specific views/queries, honor it — publish only those.
- Never expose raw credential/secret rows. If a query would surface sensitive data, flag it
  and ask rather than publish.

## Deploying — three universal paths (pick what the host supports)

Read the destination's `provider`, `method`, `site`, and `secret_name`. Export the token from
the named secret, then deploy:

1. **Provider CLI** (`method: cli`) — the host's own command. Examples:
   - Netlify: `netlify deploy --prod --dir <dir> --site <site>` (`NETLIFY_AUTH_TOKEN`)
   - Vercel: `vercel deploy --prod --token $TOKEN --yes`
   - Cloudflare Pages: `wrangler pages deploy <dir> --project-name <site>` (`CLOUDFLARE_API_TOKEN`)
   - GitHub Pages: `gh-pages -d <dir>` or push to the `gh-pages` branch
   - Surge: `surge <dir> <site>.surge.sh`
   - Firebase: `firebase deploy --only hosting`
2. **Git-push-to-deploy** (`method: git`) — commit the static files to the repo/branch the
   host auto-builds (Netlify/Vercel/Pages/Render watch a branch). Use the git discipline from
   `get_reference_doc(kind="backup-strategy")` (atomic commit, `--force-with-lease`).
3. **Object-store / file sync** (`method: sync`) — the catch-all for any host that serves
   files from a bucket or directory: `aws s3 sync <dir> s3://<bucket>` (+ CloudFront),
   `rclone copy <dir> <remote>:<path>`, or `rsync` to a server+nginx.

If the provider isn't one you recognize, fall back to its documented static-deploy command,
or to `git`/`sync` — the method is what matters, not the brand. Auth always comes from the
named secret; never hardcode a token.

## Setup → verify → then auto

Follow the same set-up-then-prove flow as backup:

1. **Configure** (`action: "configure"`) — if the strategy/destination isn't set yet, update
   `<config>.publish` with `enabled=true`, `mode="agent"`, the destination(s), and the
   token's `secret_name`. If critical details are missing, ask in this chat and write
   `publish/status.json` with state `configured_not_verified`. Do not publish yet.
2. **Verify** — on the first real publish, deploy, fetch the returned URL to confirm it loads,
   and only then mark `published`. Record the URL in both the destination and the top-level
   `url`.
3. Auto-republish (the post-run Pulse step) only runs against a **verified** destination, and
   only when the source artifacts changed.

## Always write `publish/status.json`

Before you finish — even on failure:

```json
{
  "version": 1,
  "state": "configured_not_verified | publishing | published | stale | failed",
  "url": "<public URL of the latest publish>",
  "last_published_at": "<ISO when a publish succeeded>",
  "last_attempt_at": "<ISO>",
  "last_source_hash": "<hash of the published artifacts>",
  "destinations": [
    { "id": "<id>", "provider": "<provider>", "method": "cli|git|sync", "url": "<url>", "state": "published|failed|skipped", "error": "<if failed>" }
  ],
  "last_error": "<empty on success; concise on failure>",
  "updated_at": "<ISO>"
}
```

Do not write operational publish status into `workflow.json`/the CoS config — only into
`publish/status.json`. If a destination is missing credentials or setup, mark it `failed` and
continue with any others.

## Discipline

- Provider-agnostic: deploy what the config names; don't assume a host.
- Static only: snapshot the dashboard, never ship the DB or stand up a server.
- Confirm public scope before the first publish; never expose secrets or raw sensitive rows.
- One destination missing setup never blocks the others.
