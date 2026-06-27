# Publish strategy — share HTML artifacts at a public URL

You publish a workflow's (or the org's) **HTML artifacts** to a **public URL** on a static
host. This is the share-twin of backup: backup keeps things safe; publish makes them visible.
You are **provider-agnostic** — you deploy to whatever static host the config names, using its
CLI / git / file-sync, and you record the resulting URL. Never invent a destination; the
config is the contract.

## What you publish

For a **workflow**, publish **BOTH** artifacts — the dashboard **and** the Pulse log. This is
**mandatory** unless the user explicitly says "dashboard only" (or "pulse only"). Do not skip
the Pulse log just because the saved `publish.targets` only lists the dashboard — if both
should go out, **update `targets` to include both** before you build. Deploy `dashboard.html`
AND `pulse.html`; if only one file ends up on the host, you did it wrong.

For **org-level Chief of Staff / Org Pulse**, publish **BOTH org pages**:
`pulse/goals.html` as `goals.html` and `pulse/org-pulse.html` as `pulse.html`, plus an
`index.html` wrapper with Goals | Pulse navigation. There is no workflow dashboard for the
org-level publish path unless the user explicitly asks for one.

Use the same workflow-style config/status split:

- workflow publish config/status: `workflow.json.publish` + `publish/status.json`
- org publish config/status: `pulse/publish.json` + `pulse/publish/status.json`

- **Reporting dashboard** (`reports/`) — **live** HTML: it calls `window.report.query(sql)`
  against `db/db.sqlite` inside the app, which doesn't exist on a static host. **Generate a
  static snapshot** first (next section) → `dashboard.html`.
- **Pulse log** (`builder/improve.html`, or the org's `pulse/org-pulse.html`) — a
  **self-contained** HTML document → `pulse.html`. Publish it as-is (after the theme step).

For workflow publish, deploy three files: `dashboard.html`, `pulse.html`, and an
**`index.html` wrapper** with a **top nav** (Dashboard | Pulse) over a single iframe —
clicking a tab swaps the iframe's source. For org publish, use the same wrapper pattern with
Goals | Pulse and point the first tab at `goals.html`. This gives each view full width, avoids
the double-scroll / collapse problems of a side-by-side embed, and never modifies the two
inner pages:

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

## Private by default — a simple password gate

Publishing puts the data on a URL, so **default to private.** The catch: no major static host
gives you a simple shared password for free — Netlify/Vercel site passwords are paid, and
Cloudflare/Azure "private" means a full **login** (OAuth/email), not a password. So don't reach
for those by default, and **never ask the user for an access token** to make a page private.

Instead, gate it **client-side** with a passphrase — free, one command, identical on every
host. Encrypt the static HTML with **StatiCrypt** *after* baking + theming, in the staging dir,
in **one invocation** so a single unlock covers the whole site for the browser session:

```
cd /tmp/publish-<workflow>
npx staticrypt dashboard.html pulse.html index.html \
  -p "$SECRET_PUBLISH_PASSWORD" --remember --salt <fixed-32-hex> -d .
```

Then deploy the (now-encrypted) files. Use `--remember` **plus a shared `--salt`** (any fixed
32-hex string, reused across all three files) so the unlock carries across the nav and its
iframes in one browser session — the viewer types the password **once** on `index.html`.
Verify after deploy; if a frame still prompts, inline the two views into the single nav page
and encrypt just that one file.

**Store the password as a named secret — never in plaintext.** For workflow publish, put it in
the workflow's secret store (e.g. `PUBLISH_PASSWORD`) and read it as `$SECRET_PUBLISH_PASSWORD`,
so the **auto-republish (Pulse)** step can re-encrypt without the user. For org publish, use a
user/global secret with the same named-secret convention. For workflow publish, record only
`visibility: "private"` and the `secret_name` in `workflow.json.publish` / `publish/status.json`.
For org publish, record those same non-secret fields in `pulse/publish.json` /
`pulse/publish/status.json`. **Never the password itself.**

**Honest limit:** a client-side gate is good *casual* privacy (keeps it out of public/search
view, needs the password to read) but not strong security — a weak password on the encrypted
file can be brute-forced offline. We already forbid publishing raw secrets; for genuinely
sensitive data, offer a **login host** instead.

**Stronger private (opt-in, only if the user asks):**
- **Azure Static Web Apps** — built-in auth + route authz on the **free** tier
  (`staticwebapp.config.json`, `"allowedRoles": ["authenticated"]`); login via GitHub / Entra.
- **Cloudflare Pages + Cloudflare Access** — free Zero Trust (≤50 users): deploy to Pages, then
  put an Access policy (email OTP / Google) in front.

**Public (opt-in, only when the user says so):** skip the gate and deploy the baked files as-is.
Fine when the dashboard has no sensitive data.

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

**Then come back and persist state INTO the workflow folder — this is not optional.** The
`/tmp` dir is throwaway; the record the UI reads lives in the workflow. A deploy that succeeds
but leaves no `publish/status.json` + no `workflow.json.publish` block shows up as a **grey
"not configured"** dot — i.e. it looks like you never published. After the CLI returns the URL,
return to the workflow folder and, before you finish: (1) set `workflow.json.publish.enabled =
true` with the destination + top-level `url`, and (2) write `publish/status.json` with
`state: "published"`, the `url`, and `last_source_hash` (see the two sections below). Never
write these into the `/tmp` staging dir.

For **org-level publish**, come back and persist state under `pulse/` instead:

- update `pulse/publish.json` with `enabled=true`, destinations, targets, visibility, and
  the top-level URL,
- write `pulse/publish/status.json` with `state: "published"`, the URL,
  `last_source_hash`, destination results, and updated time,
- never write org publish state into a workflow's `workflow.json`.

For workflow publish, read the destination's `provider`, `method`, and `site` from
`workflow.json.publish`. For org publish, read the same destination fields from
`pulse/publish.json`. Then deploy:

1. **Provider CLI** (`method: cli`) — the default, and the host's own CLI **handles its own
   auth**. Almost every major static host ships a CLI, most installable with `npm i -g`.
   **Do NOT ask the user for an access token / API key.** The auth path is always: *install the
   CLI → the user runs `<cli> login` once (browser) → you deploy.* Before deploying:
   - **Auto-check + install.** As soon as the user names a host, check its CLI
     (`command -v vercel`). If it's missing, **install it for them**: say what you're doing
     ("Installing the Vercel CLI…") and run the table's install command (`npm i -g vercel`).
     Don't just hand over the command and wait — drive it. Pick the command for the host they
     named: Vercel → `npm i -g vercel`, Netlify → `npm i -g netlify-cli`, Cloudflare
     Pages/R2 → `npm i -g wrangler`, Firebase → `npm i -g firebase-tools`, AWS → `brew install
     awscli`. If the global install fails (e.g. `EACCES` on the npm prefix, or needs sudo), then
     fall back to giving the user the exact command to run. Announce, don't silently install.
   - **Check it's logged in** (`netlify status`, `vercel whoami`, `firebase login:list`,
     `wrangler whoami`, …). **You do NOT handle tokens or secrets — the CLI uses its own
     stored login session.** If it isn't authenticated, tell the user to run the one-time
     login command (e.g. `netlify login`); it opens a browser, so you can't do it for them.
     Then deploy. If a login command stalls, the fix is to re-run `<cli> login` — **not** to
     fall back to pasting a token.

   **Default to a FREE CLI host** unless the user asks for a specific one or already uses a
   paid provider. Good free defaults: **Surge** (simplest, instant `*.surge.sh`), **Cloudflare
   Pages** (most generous — unlimited bandwidth), or **Netlify**. The table is ordered free-first;
   AWS is the only paid option — only use it if the user already lives in AWS or asks.

   | Host | Tier | Install | Log in (one-time, user) | Deploy |
   |------|------|------|------|------|
   | Surge | **free** | `npm i -g surge` | `surge login` | `surge <dir> <site>.surge.sh` |
   | Cloudflare Pages | **free** (unlimited bandwidth) | `npm i -g wrangler` | `wrangler login` | `wrangler pages deploy <dir> --project-name <site>` |
   | Netlify | **free** | `npm i -g netlify-cli` | `netlify login` | `netlify deploy --prod --dir <dir>` |
   | Vercel | **free** (Hobby) | `npm i -g vercel` | `vercel login` | `vercel deploy --prod --yes` |
   | Firebase Hosting | **free** (Spark) | `npm i -g firebase-tools` | `firebase login` | `firebase deploy --only hosting` |
   | GitHub Pages | **free** (public sites) | `gh` CLI (`brew install gh`) | `gh auth login` | `gh-pages -d <dir>` or push the `gh-pages` branch |
   | Azure Static Web Apps | **free** (+ built-in login auth) | `npm i -g @azure/static-web-apps-cli` | `az login` | `swa deploy <dir> --env production` |
   | Cloudflare R2 | free storage, **+setup** | `npm i -g wrangler` | `wrangler login` | `wrangler r2 object put <bucket>/<key> --file <f>` (serve via the public r2.dev / custom domain) |
   | AWS S3 + CloudFront | **paid** (pay-as-you-go) | `brew install awscli` | `aws configure` | `aws s3 sync <dir> s3://<bucket>` |

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
   the right config: `workflow.json.publish` for workflow publish, or `pulse/publish.json`
   for org publish (enabled, mode, targets, destination provider/method/site, visibility,
   and secret name only). For a CLI host, **proactively suggest the one-time CLI install** (exact command from
   the table) and confirm the CLI is installed and **logged in** (`<cli> login`) — the CLI
   handles auth, so you don't store tokens. If critical details are missing, ask in this chat
   and write the right status (`publish/status.json` for workflow, or
   `pulse/publish/status.json` for org) with state `configured_not_verified`. Do not publish yet.

   **Edit workflow.json safely.** For workflow publish, change ONLY the `publish` block and preserve every other
   field. The `targets` value must be a JSON array (of strings like `"report"`/`"pulse"`, or
   objects). After writing, re-read it with a JSON parser
   (`python3 -c "import json; json.load(open('workflow.json'))"`) to confirm it still parses —
   a malformed workflow.json drops the workflow's config and can hide the workflow from the UI.
   For org publish, write valid JSON in `pulse/publish.json` and do not touch workflow
   manifests.
2. **Verify** — on the first real publish, deploy, fetch the returned URL to confirm it loads,
   and only then mark `published`. Record the URL in both the destination and the top-level
   `url`.
3. Auto-republish (the post-run Pulse step) only runs against a **verified** destination, and
   only when the source artifacts changed.

## Always write publish status

For workflow publish, before you finish — even on failure — write `publish/status.json`.
For org publish, write the same shape to `pulse/publish/status.json`:

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

Do not write operational publish status into workflow/org config files. Use
`publish/status.json` for workflow publish and `pulse/publish/status.json` for org publish.
If a destination is missing credentials or setup, mark it `failed` and continue with any
others.

**Get `last_source_hash` right or the dot lies.** The backend computes the source hash itself
(a sha256 over `builder/improve.html`, `db/db.sqlite`, and `reports/`) and reports it as
`current_source_hash` in the workflow publish status. For org publish, hash
`pulse/goals.html` and `pulse/org-pulse.html` deterministically and record that hash in the
`pulse/publish/status.json` status file. Set `last_source_hash` to the current source hash you just
published. If you write any other string, a successful publish immediately reads as
**`stale`** (amber) and Pulse will keep re-publishing. If you genuinely can't obtain it,
leave `last_source_hash` empty — the dot stays green `published`, only change-detection is
disabled. The two states this controls:
- `published` (green) = config enabled + status `published` + hash matches.
- `stale` (amber) = published but the source changed since — Pulse re-publishes on the next run.

## Discipline

- Provider-agnostic: deploy what the config names; don't assume a host.
- Static only: snapshot the dashboard, never ship the DB or stand up a server.
- Confirm public scope before the first publish; never expose secrets or raw sensitive rows.
- One destination missing setup never blocks the others.
