## Workflow log conventions

Canonical conventions shared by every `/review-*` and `/improve-*` skill, the post-run monitor, and any chat-driven fix that touches the log. Load this once; the individual skills point here instead of restating it.

### One log: `builder/improve.html`

The workflow keeps a **single durable log** — `builder/improve.html` — the workflow's journal and the user's primary window into it. Everything the user should be able to see later goes here, in one place:

- **applied or proposed changes** (what `/improve-*`, harden/replan, and chat fixes did, and why),
- **review findings** (what `/review-*` flagged — recommendations, REVIEW = recommend, do NOT apply),
- **run notes and the recent-run log** (what happened on recent runs),
- **monitor observations** (post-run regressions / drift the monitor caught),
- **user rules** (authoritative constraints the user stated).

`builder/review.html` is **legacy**. If you encounter one with unresolved findings, fold them into `builder/improve.html` as open-finding entries, then stop writing to it. Do not create new `review.html` files.

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Call `get_reference_doc(kind="html-output")` for the style baseline. The target layout is the single-log view: a header with the workflow's current health, **signal tiles** (metrics as numbers), a compact **recent-runs** strip, a **newest-first timeline** of entries, and an **archive index**. Match that structure and that level of polish.

### Newest on top — always

New entries go at the **top** of the timeline, not appended at the bottom. The file carries a stable anchor comment `<!-- LOG ENTRIES: newest first -->` directly below the header/tiles; insert each new entry immediately after it with `diff_patch_workspace_file`. Never reorder or rewrite existing entries except to close out an open finding (below). **Always read the existing file first** so you continue its style and don't duplicate entries.

### Entry kinds

Each entry is a small card: a date, a kind tag, a one-line title, and a short prose body (2–4 sentences, plain language — explain *what* and *why*, link the evidence file or changelog entry when relevant). Use these kinds:

- **Run** — a one-line row in the recent-runs strip: run id, status, key numbers (tests, eval, cost), and a short note only when something stands out. Routine runs stay terse; flag a run only when it regressed.
- **Monitor** — a post-run observation: what changed in the output and the most likely cause, correlated against the plan changelog ("output regressed at run N; you tightened step X two runs earlier — likely cause").
- **Decision** — a change applied or proposed, with the one-line rationale and the file(s) touched. If it fixes an open finding, close that finding out (below).
- **User rule** — a constraint the user stated. Mark it clearly as authoritative ("USER RULE — authoritative") so future agents treat it as a hard constraint, never silently override it. This replaces the old `source: "user"` field — say it in words.
- **Note** — a freeform observation or watchpoint that explains weird runs ("staging UI is mid-redesign, expect selector churn through ~June 20 — not a workflow bug").
- **Open finding** — something wrong that is not yet fixed. Give it a short stable anchor id (e.g. `id="of-2026-06-07-screenshots"`) **only so a later Decision can mark it resolved** — that is the one place an id earns its keep. No other entry kind needs an id.

### Closing out an open finding

When a change fixes an open finding, edit that finding's card in place to add a resolved line — don't delete it, don't open a duplicate:

```
Resolved 2026-06-09 — added a non-empty-screenshot pre-validation rule to audit-finalizer.
```

Reference the finding by its anchor id (or, if it has none, by its date + title). This keeps the "what's still outstanding vs. what's been handled" view honest.

### Metrics as prose, never charts

Render current metric values as readable tiles or lines — the value plus its movement in words (`eval 0.86 ▲ from 0.79`, `cost ¢19 ▲ from ¢12`). Read them from `planning/metrics.json`, `db/metrics_history.jsonl`, and `scores/evaluation/`. These files are the deterministic source of truth — quote them, never fabricate a number or a trend.

### Keep the active file small

The log must not grow without bound. When `builder/improve.html` passes roughly **800 lines, 60 KB, or 20 timeline entries**, move older **resolved** findings, superseded decisions, and routine run rows into a monthly archive `builder/improve-archive/YYYY-MM.html`, leaving a one-row entry in the Archive Index (date range, count, any still-unresolved ids). **Never archive** open findings, user rules, current notes, or the latest few entries — the active file should always answer "what's the state of this workflow right now and what still needs attention."

### Legacy `.md` / structured-block migration (one-time)

Some workspaces still have legacy `builder/improve.md` / `builder/review.md`, or an `improve.html` full of old ```improve-decision``` fenced JSON blocks and `F-…`/`I-…` ids. When you open such a file: read it in full, carry every unresolved finding and still-relevant decision forward as readable timeline entries in the new format, then delete the `.md` (via `execute_shell_command`) or replace the JSON blocks with prose cards. Do this once, before writing new entries, so nothing is lost. The structured JSON schema and the dual `F-/I-` id system are retired — write readable prose instead.

### Starter HTML skeleton (copy this exactly)

`builder/improve.html` renders in a full sandboxed iframe — the same way reports render — so it supports real CSS, web fonts, and themes. There is no excuse for a plain or ugly log: match the polish below. When bootstrapping a new log, write this document verbatim, fill the header/profile, and leave the `<!-- LOG ENTRIES: newest first -->` anchor in place. On every later turn, insert new entry cards **immediately after that anchor** (newest on top). Keep the CSS block stable so the look stays consistent run to run.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title><!-- WORKFLOW NAME --> · workflow log</title>
<style>
  :root{--bg:#f6f6f4;--surface:#fff;--ink:#1a1a18;--ink-soft:#5b5b54;--line:#e7e6e1;--line-strong:#d8d7d0;
    --accent:#3a5a40;--accent-soft:#e7efe6;--warn:#9a6b00;--warn-soft:#fbf1da;--bad:#9b2c2c;--bad-soft:#fbe9e7;
    --user:#3b4a8c;--user-soft:#e9ecf8;--mono:"SF Mono",ui-monospace,Menlo,monospace;
    --sans:"Inter",-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;}
  *{box-sizing:border-box} body{margin:0;background:var(--bg);color:var(--ink);font-family:var(--sans);line-height:1.5}
  .wrap{max-width:860px;margin:0 auto;padding:40px 28px 96px}
  .top{display:flex;justify-content:space-between;align-items:flex-start;gap:24px}
  h1{font-size:30px;letter-spacing:-.02em;margin:6px 0 0;font-weight:680}
  .crumb{font:500 12px/1 var(--mono);letter-spacing:.04em;color:var(--ink-soft);text-transform:uppercase}
  .pill{display:inline-flex;align-items:center;gap:7px;font:600 13px/1 var(--sans);padding:8px 14px;border-radius:999px}
  .pill.ok{background:var(--accent-soft);color:var(--accent)} .pill.warn{background:var(--warn-soft);color:var(--warn)}
  .pill.bad{background:var(--bad-soft);color:var(--bad)} .dot{width:7px;height:7px;border-radius:50%;background:currentColor}
  .chips{display:flex;flex-wrap:wrap;gap:6px;margin-top:14px}
  .chip{font:500 12px/1 var(--sans);padding:5px 10px;border-radius:999px;background:var(--surface);border:1px solid var(--line-strong);color:var(--ink-soft)}
  .chip b{color:var(--ink)}
  .tiles{display:grid;grid-template-columns:repeat(4,1fr);gap:1px;background:var(--line);border:1px solid var(--line);border-radius:14px;overflow:hidden;margin:28px 0 32px}
  .tile{background:var(--surface);padding:16px} .tile .k{font:500 11px/1 var(--mono);letter-spacing:.05em;text-transform:uppercase;color:var(--ink-soft)}
  .tile .v{font-size:23px;font-weight:680;margin-top:9px} .tile .d{font:500 12px/1.2 var(--sans);margin-top:5px;color:var(--ink-soft)}
  .up{color:var(--accent)} .down{color:var(--bad)}
  .seclabel{font:600 12px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-soft);margin:0 0 14px}
  .runs{border:1px solid var(--line);border-radius:12px;overflow:hidden;background:var(--surface);margin-bottom:34px}
  .run{display:flex;align-items:center;gap:14px;padding:11px 16px;border-top:1px solid var(--line);font:500 13px/1 var(--mono);color:var(--ink-soft)}
  .run:first-child{border-top:none} .run.flag{background:var(--warn-soft)} .run .id{color:var(--ink);font-weight:700;width:38px}
  .run .st{display:inline-flex;align-items:center;gap:6px;width:96px} .run .st.ok{color:var(--accent)} .run .st.warn{color:var(--warn)}
  .run .st .d{width:6px;height:6px;border-radius:50%;background:currentColor} .run .col{width:78px} .run .col b{color:var(--ink)}
  .run .note{color:var(--warn);font:500 12px/1 var(--sans)} .run .ago{margin-left:auto;color:var(--line-strong)}
  .entry{background:var(--surface);border:1px solid var(--line);border-left:3px solid var(--line-strong);border-radius:12px;padding:18px 20px;margin-bottom:14px}
  .entry.monitor{border-left-color:var(--warn)} .entry.agent{border-left-color:var(--accent)}
  .entry.user{border-left-color:var(--user);background:var(--user-soft)} .entry.open{border-left-color:var(--bad)} .entry.note{border-left-color:var(--line-strong)}
  .ehead{display:flex;align-items:center;gap:10px;margin-bottom:9px}
  .tag{font:700 10px/1 var(--mono);letter-spacing:.07em;text-transform:uppercase;padding:4px 7px;border-radius:5px}
  .tag.monitor{background:var(--warn-soft);color:var(--warn)} .tag.agent{background:var(--accent-soft);color:var(--accent)}
  .tag.user{background:var(--user-soft);color:var(--user)} .tag.open{background:var(--bad-soft);color:var(--bad)} .tag.note{background:#efeee9;color:var(--ink-soft)}
  .etitle{font-weight:620;font-size:15px} .when{margin-left:auto;font:500 12px/1 var(--mono);color:var(--ink-soft)}
  .entry p{margin:0;font-size:14.5px} .entry .meta{margin-top:11px;font:500 12px/1.4 var(--mono);color:var(--ink-soft)}
  .archive{border:1px solid var(--line);border-radius:12px;background:var(--surface);overflow:hidden}
  .arow{display:flex;gap:14px;align-items:center;padding:14px 18px;border-top:1px solid var(--line);font-size:14px;color:var(--ink-soft)}
  .arow:first-child{border-top:none} .arow b{color:var(--ink)} .arow .n{margin-left:auto;font:500 12px/1 var(--mono)}
  footer{margin-top:44px;padding-top:18px;border-top:1px solid var(--line);font:500 12px/1.4 var(--mono);color:var(--ink-soft)}
</style>
</head>
<body><div class="wrap">

  <div class="top">
    <div><div class="crumb">workflow · log</div><h1><!-- WORKFLOW NAME --></h1></div>
    <!-- health pill: ok | warn | bad -->
    <div class="pill ok"><span class="dot"></span>Healthy</div>
  </div>
  <div class="chips">
    <span class="chip">Type <b><!-- primary type --></b></span>
    <span class="chip">Oversight <b><!-- oversight_mode --></b></span>
    <span class="chip">Last run <b>—</b></span>
  </div>

  <!-- PROFILE: keep the Workflow Profile + behavioural implications here as short prose. -->

  <!-- SIGNAL TILES: current metric values as numbers + delta in words. Read from
       planning/metrics.json + db/metrics_history.jsonl; never invent. -->
  <div class="tiles">
    <div class="tile"><div class="k">—</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>

  <p class="seclabel">Recent runs</p>
  <div class="runs">
    <!-- one .run row per recent run; add .flag + a .note when something stands out -->
  </div>

  <p class="seclabel">Latest — newest first</p>
  <!-- LOG ENTRIES: newest first -->
  <!-- Insert each new entry card immediately below this anchor. Card kinds:
       <div class="entry monitor"><div class="ehead"><span class="tag monitor">Monitor</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry agent"><div class="ehead"><span class="tag agent">Agent · hardened</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry user"><div class="ehead"><span class="tag user">User rule · authoritative</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry note"><div class="ehead"><span class="tag note">Note</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry open" id="of-YYYY-MM-DD-slug"><div class="ehead"><span class="tag open">Open finding</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       To close an open finding, add inside its card: <p class="meta">Resolved YYYY-MM-DD — how.</p> -->

  <p class="seclabel" style="margin-top:36px">Archive</p>
  <div class="archive"><!-- one .arow per monthly archive file once you start rolling entries off --></div>

  <footer>generated by the workflow agent · newest first · older detail archived monthly</footer>

</div></body>
</html>
```
