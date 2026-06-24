# Publish strategy — share HTML artifacts at a public URL

You publish a workflow's (or the org's) **HTML artifacts** to a **public URL** on a static
host. This is the share-twin of backup: backup keeps things safe; publish makes them visible.
You are **provider-agnostic** — you deploy to whatever static host the config names, using its
CLI / git / file-sync, and you record the resulting URL. Never invent a destination; the
config is the contract.

## What you publish

Publish **BOTH** artifacts — the dashboard **and** the Pulse log. This is **mandatory** unless
the user explicitly says "dashboard only" (or "pulse only"). Do not skip the Pulse log just
because the saved `publish.targets` only lists the dashboard — if both should go out, **update
`targets` to include both** before you build. Deploy `dashboard.html` AND `pulse.html`; if
only one file ends up on the host, you did it wrong.

- **Reporting dashboard** (`reports/`) — **live** HTML: it calls `window.report.query(sql)`
  against `db/db.sqlite` inside the app, which doesn't exist on a static host. **Generate a
  static snapshot** first (next section) → `dashboard.html`.
- **Pulse log** (`builder/improve.html`, or the org's `pulse/org-pulse.html`) — a
  **self-contained** HTML document → `pulse.html`. Publish it as-is (after the theme step).

Deploy three files: `dashboard.html`, `pulse.html`, and an **`index.html` wrapper** with a
**top nav** (Dashboard | Pulse) over a single iframe — clicking a tab swaps the iframe's
source. This gives each view full width, avoids the double-scroll / collapse problems of a
side-by-side embed, and never modifies the two inner pages:

```html
<!doctype html><html class="dark" data-theme="dark"><head>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="color-scheme" content="dark">
  <style>
    html,body{margin:0;height:100%;font-family:system-ui,-apple-system,sans-serif;background:#141413;color:#e8e7e2}
    nav{display:flex;align-items:center;gap:.25rem;padding:.5rem .75rem;border-bottom:1px solid #2c2b28}
    nav button{border:0;background:transparent;padding:.4rem .85rem;border-radius:.4rem;
      cursor:pointer;font:inherit;color:inherit;opacity:.65}
    nav button.active{background:#ffffff22;opacity:1;font-weight:600}
    iframe{border:0;width:100%;height:calc(100vh - 49px);display:block}
  </style>
</head><body>
  <nav>
    <button data-src="dashboard.html" class="active">Dashboard</button>
    <button data-src="pulse.html">Pulse</button>
  </nav>
  <iframe id="view" src="dashboard.html" title="view"></iframe>
  <script>
    var view=document.getElementById('view');
    // Inner pages already force dark; re-assert both hooks on each load as a backstop.
    view.addEventListener('load',function(){try{var d=view.contentDocument.documentElement;d.classList.add('dark');d.dataset.theme='dark';}catch(e){}});
    document.querySelectorAll('nav button[data-src]').forEach(function(b){
      b.onclick=function(){view.src=b.dataset.src;document.querySelectorAll('nav button[data-src]').forEach(function(x){x.classList.toggle('active',x===b)});};
    });
  </script>
</body></html>
```

(Prefer a **left sidebar** instead of a top nav if the user asks — same idea, nav on the
left, iframe filling the rest.) The wrapper above is **dark only** and re-asserts dark on the
iframe each load; apply the matching dark shim (below) to `dashboard.html` and `pulse.html` so
they force dark too. Never
publish secrets, `db/db.sqlite` raw, credentials, or `.env`/key files.

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

## Force the published HTML to DARK (both artifacts)

The output must be **dark only**, matching the app. Match exactly what the app sets on `<html>`:

- The app themes via **Tailwind's `dark` class** (`ThemeContext` does `classList.add('dark')`).
- Report/dashboard widgets honor **both** — `HtmlWidgetFrame` sets `class="dark"` **and**
  `data-theme="dark"`.
- The Pulse-log skeleton keys on **`data-theme="dark"`**.

So set **both** hooks, hard-coded to dark. Do **NOT** use `prefers-color-scheme` (it follows
the viewer's OS, usually light) and do **NOT** add a light/auto toggle — dark only.

For **every** published HTML file (the Pulse log AND the report snapshot), inject this in
`<head>` BEFORE the page's own styles/scripts:

```html
<meta name="color-scheme" content="dark">
<script>
  var de = document.documentElement;
  de.classList.add('dark');   // Tailwind / :root.dark report widgets + app parity
  de.dataset.theme = 'dark';  // data-theme="dark" widgets + the Pulse-log skeleton
</script>
```

Setting both is exactly what the in-app `HtmlWidgetFrame` does, so the page reuses the existing
dark CSS. (If a widget has no dark styling at all, add a minimal dark palette under
`html.dark, html[data-theme="dark"]`.)

## Every publish rebuilds from source — never redeploy a stale file

A publish (including a **re-publish** and the auto-republish) ALWAYS regenerates the
artifacts fresh, then deploys:
- **Re-snapshot** the dashboard (re-run the queries → current data), **re-inject the theme
  shim**, and **rebuild the `index.html` wrapper**. Do NOT redeploy a previously-baked
  `dashboard.html` — stale data, a missing theme, and the old layout persist otherwise.
- **Honor "publish both."** If the existing `publish.targets` lists only one artifact but
  both should go out (the default), update `targets` to include both before rebuilding — a
  re-publish must not silently stay single-artifact just because the old config did.
- After deploy, open the URL and confirm BOTH panes render and the page respects dark mode.

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

**Stage outside the workspace first.** The workspace folder is write-guarded, so deploy CLIs
that write working files into it (`.netlify/`, build output, lock files) will fail. Copy the
finished static files — `dashboard.html`, `pulse.html`, `index.html` — into a scratch dir
**outside** the workspace (e.g. `/tmp/publish-<workflow>/`), and run the deploy CLI from there
with an explicit `--dir`, skipping any build step (the files are already built). Don't try to
`cd` inside the workspace or let the CLI build in place.

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
