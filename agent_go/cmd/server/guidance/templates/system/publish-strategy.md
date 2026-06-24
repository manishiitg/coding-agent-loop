# Publish strategy — share HTML artifacts at a public URL

You publish a workflow's (or the org's) **HTML artifacts** to a **public URL** on a static
host. This is the share-twin of backup: backup keeps things safe; publish makes them visible.
You are **provider-agnostic** — you deploy to whatever static host the config names, using its
CLI / git / file-sync, and you record the resulting URL. Never invent a destination; the
config is the contract.

## What you publish

Publish **both** artifacts by default. Use the config's `targets`; if `targets` is empty or
absent, publish both — do **not** publish only one unless the user explicitly asked for one.

- **Reporting dashboard** (`reports/`) — **live** HTML: it calls `window.report.query(sql)`
  against `db/db.sqlite` inside the app, which doesn't exist on a static host. **Generate a
  static snapshot** first (next section).
- **Pulse log** (`builder/improve.html`, or the org's `pulse/org-pulse.html`) — a
  **self-contained** HTML document. Publish it as-is (after the theme step below).

When publishing both, deploy three files: `dashboard.html` (the report snapshot),
`pulse.html` (the Pulse log), and a small **`index.html` wrapper** that shows them together in
a responsive layout via two `<iframe>`s — keep them as separate files (iframes isolate their
CSS/scripts; never merge the two documents). Wrapper:

```html
<!doctype html><html><head>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="color-scheme" content="light dark">
  <style>
    html,body{margin:0;height:100%}
    .grid{display:grid;grid-template-columns:1fr 380px;height:100vh}
    .grid iframe{border:0;width:100%;height:100%;display:block}
    /* Mobile: iframes can't auto-size to content, so let the PAGE scroll and give
       each iframe an explicit height (don't use `auto` rows — the iframe collapses). */
    @media (max-width:820px){
      html,body{height:auto}
      .grid{display:block;height:auto}
      .grid iframe{height:88vh}
    }
  </style>
</head><body>
  <div class="grid">
    <iframe src="dashboard.html" title="Dashboard"></iframe>
    <iframe src="pulse.html" title="Pulse"></iframe>
  </div>
</body></html>
```

Desktop/tablet → dashboard main + Pulse as a right rail; mobile (≤820px) → stacked, dashboard
on top. Apply the theme shim (below) to `dashboard.html` and `pulse.html` (the iframed pages),
not the wrapper. Never publish secrets, `db/db.sqlite` raw, credentials, or `.env`/key files.

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

## Make the published HTML theme-aware (both artifacts)

In the app, dark mode is set by the app **injecting `data-theme="dark"` onto `<html>`** — a
published static page has no app, so it would render **light only**. For **every** published
HTML file (the Pulse log AND the report snapshot), inject this shim in `<head>` so the page
follows the **viewer's** system theme:

```html
<meta name="color-scheme" content="light dark">
<script>
  document.documentElement.dataset.theme =
    matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
</script>
```

This reuses the page's existing `html[data-theme="dark"]` CSS, so dark-mode viewers get dark
and light-mode viewers get light — no app needed. (If a page has no dark CSS at all, also add
a minimal dark palette under `@media (prefers-color-scheme: dark)`.)

## Public or private? Ask first

Publishing puts the data on a URL. **Before choosing a host, ask the user whether the page
should be public (anyone with the link) or private (behind a login).** For a dashboard with
real data, recommend **private** by default.

**Private for free — prefer these when the data is sensitive:**
- **Azure Static Web Apps** — built-in auth + route authorization on the **free** tier. Add a
  `staticwebapp.config.json` route rule with `"allowedRoles": ["authenticated"]` so the site
  requires login (GitHub / Microsoft Entra ID). No extra service. Cleanest free-private option.
- **Cloudflare Pages + Cloudflare Access** — Cloudflare Zero Trust **free** tier (≤50 users):
  deploy to Pages, then put an Access policy (email OTP / Google / etc.) in front of it.

**Private with a workaround:**
- **Netlify** (free) — HTTP Basic Auth via a `_headers` file (per-path, manual). Site-wide
  password is a paid (Pro) feature.
- **Vercel** — "Vercel Authentication" restricts to team members on all plans, but the free
  Hobby plan allows only one external user; a shared password is a paid add-on.

**Public-only-free / paid-private** — GitHub Pages (private = Enterprise), Surge, Firebase
(add app-level auth yourself). Use these when the page is meant to be public.

Whatever the choice, before the first publish of a destination:
- State plainly what will be visible (which artifacts; for the dashboard, which queries/rows)
  and confirm scope. If the config scopes the report to specific views/queries, honor it.
- Never expose raw credential/secret rows. If a query would surface sensitive data, either
  scope it out or use a private host — ask rather than publish.

## Deploying — three universal paths (pick what the host supports)

Read the destination's `provider`, `method`, and `site`, then deploy:

1. **Provider CLI** (`method: cli`) — the host's own CLI, which **handles its own auth**.
   Almost every major static host ships a CLI, most installable with `npm i -g`. Before
   deploying:
   - **Check it's installed** (`command -v netlify`). If missing, tell the user the exact
     install command (table) and ask them to run it — CLIs install per machine, a one-time
     user step; never install silently.
   - **Check it's logged in** (`netlify status`, `vercel whoami`, `firebase login:list`,
     `wrangler whoami`, …). **You do NOT handle tokens or secrets — the CLI uses its own
     stored login session.** If it isn't authenticated, tell the user to run the one-time
     login command (e.g. `netlify login`); it opens a browser, so you can't do it for them.
     Then deploy.

   | Host | Install | Log in (one-time, user) | Deploy |
   |------|------|------|------|
   | Netlify | `npm i -g netlify-cli` | `netlify login` | `netlify deploy --prod --dir <dir>` |
   | Vercel | `npm i -g vercel` | `vercel login` | `vercel deploy --prod --yes` |
   | Cloudflare Pages | `npm i -g wrangler` | `wrangler login` | `wrangler pages deploy <dir> --project-name <site>` |
   | Firebase Hosting | `npm i -g firebase-tools` | `firebase login` | `firebase deploy --only hosting` |
   | Surge | `npm i -g surge` | `surge login` | `surge <dir> <site>.surge.sh` |
   | Azure Static Web Apps | `npm i -g @azure/static-web-apps-cli` | `az login` | `swa deploy <dir> --env production` |
   | AWS S3 + CloudFront | `brew install awscli` | `aws configure` | `aws s3 sync <dir> s3://<bucket>` |
   | GitHub Pages | `gh` CLI (`brew install gh`) | `gh auth login` | `gh-pages -d <dir>` or push the `gh-pages` branch |

   If the host isn't listed, it almost certainly still has a CLI — check its docs for the
   install + login + deploy commands; the user logs in once, you deploy.

   *(Headless/CI only: most CLIs also accept a token env var — `NETLIFY_AUTH_TOKEN`,
   `VERCEL_TOKEN`, `CLOUDFLARE_API_TOKEN`, etc. — via the destination's optional `secret_name`.
   For a person at the keyboard, prefer interactive `<cli> login`.)*
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

1. **Configure** (`action: "configure"`) — if the destination isn't set yet, add it to
   `<config>.publish` (`enabled=true`, `mode="agent"`, the destination's provider / method /
   site). For a CLI host, **proactively suggest the one-time CLI install** (exact command from
   the table) and confirm the CLI is installed and **logged in** (`<cli> login`) — the CLI
   handles auth, so you don't store tokens. If critical details are missing, ask in this chat
   and write `publish/status.json` with state `configured_not_verified`. Do not publish yet.

   **Edit workflow.json safely.** Change ONLY the `publish` block and preserve every other
   field. The `targets` value must be a JSON array (of strings like `"report"`/`"pulse"`, or
   objects). After writing, re-read it with a JSON parser
   (`python3 -c "import json; json.load(open('workflow.json'))"`) to confirm it still parses —
   a malformed workflow.json drops the workflow's config and can hide the workflow from the UI.
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
