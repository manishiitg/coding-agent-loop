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

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Call `get_reference_doc(kind="html-output")` for the style baseline, and copy the **Starter HTML skeleton** at the bottom of this doc for the exact structure and polish. Top to bottom the document reads: **two verdicts → the goal → signal tiles → recent runs → newest-first timeline → archive**.

### Two verdicts: Bug and Goal

Every workflow is judged on two independent axes, and the header shows **both** as separate pills — never collapse them into a single "health":

- **Bug** — did it *run correctly*? Errors, skipped steps, missing/empty artifacts, regressions vs the last run. A bug is fixed by **hardening**. Operational, roughly binary.
- **Goal** — is it *achieving its success criteria*? Eval scores and outcome metrics vs `soul.md`, trending over runs. A goal gap is fixed by **refining or replanning**. Continuous.

They are orthogonal: a run can be **Bug: broken** (a step silently skipped) while **Goal: on-target**, or **Bug: clean** while **Goal: short** (it runs perfectly but produces output that misses the point). You need both lenses — operational monitoring can't see a goal gap, and outcome metrics can't see a skipped step. **Health gates goal:** a run that wasn't operationally clean produces no trustworthy goal signal, so never judge the goal on a broken run.

Tag each **Monitor**, **Open finding**, and **Decision** entry with the axis it belongs to — a small **Bug** or **Goal** chip — so the timeline is filterable and the fix path is obvious (Bug → harden, Goal → refine/replan).

### The goal card

Directly under the verdicts, show **what the workflow is for**: the one-line objective plus the success criteria from `soul.md`, each with a live status — **Met / Short / At risk** — and the metric or evidence behind that status. This is what the **Goal** verdict is measured against; without it the verdict is opaque. Keep it current as criteria are met or slip. (`/define-success` seeds this from `soul.md` on bootstrap.)

### Signal tiles — grouped by verdict

Render metrics as readable tiles (value + movement in words: `eval 0.78 ▶ target 0.90`, `cost ¢19 ▲ from ¢12`), grouped into **Bug tiles** (did it run: tests executed, last-run status) and **Goal tiles** (is it achieving: eval scores vs target, outcome metrics vs success criteria). Read every number from `planning/metrics.json`, `db/metrics_history.jsonl`, and `scores/evaluation/` — the deterministic source of truth. Never fabricate a value or a trend, and never use charts.

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
    --user:#3b4a8c;--user-soft:#e9ecf8;--goal:#7a3b8c;--goal-soft:#f3e9f6;
    --mono:"SF Mono",ui-monospace,Menlo,monospace;--sans:"Inter",-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;}
  /* Dark palette — the app injects data-theme="dark" on <html> when its theme is dark. Keep this block. */
  html[data-theme="dark"]{--bg:#141413;--surface:#1d1d1b;--ink:#e8e7e2;--ink-soft:#a3a299;--line:#2c2b28;--line-strong:#3a3935;
    --accent:#7fb086;--accent-soft:#1d2a20;--warn:#d9a441;--warn-soft:#2c2410;--bad:#e0726a;--bad-soft:#2c1614;
    --user:#8a9bd8;--user-soft:#1a1f33;--goal:#c08ad0;--goal-soft:#271a2c;}
  html{color-scheme:light} html[data-theme="dark"]{color-scheme:dark}
  *{box-sizing:border-box} body{margin:0;background:var(--bg);color:var(--ink);font-family:var(--sans);line-height:1.5}
  .wrap{max-width:880px;margin:0 auto;padding:40px 28px 96px}
  .top{display:flex;justify-content:space-between;align-items:flex-start;gap:24px;flex-wrap:wrap}
  h1{font-size:30px;letter-spacing:-.02em;margin:6px 0 0;font-weight:680}
  .crumb{font:500 12px/1 var(--mono);letter-spacing:.04em;color:var(--ink-soft);text-transform:uppercase}
  .verdicts{display:flex;gap:8px;flex-wrap:wrap}
  .pill{display:inline-flex;align-items:center;gap:7px;font:600 12.5px/1 var(--sans);padding:8px 13px;border-radius:999px}
  .pill .lbl{font:700 9px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;opacity:.7}
  .pill.ok{background:var(--accent-soft);color:var(--accent)} .pill.warn{background:var(--warn-soft);color:var(--warn)} .pill.bad{background:var(--bad-soft);color:var(--bad)}
  .dot{width:7px;height:7px;border-radius:50%;background:currentColor}
  .chips{display:flex;flex-wrap:wrap;gap:6px;margin-top:16px} .chip{font:500 12px/1 var(--sans);padding:5px 10px;border-radius:999px;background:var(--surface);border:1px solid var(--line-strong);color:var(--ink-soft)} .chip b{color:var(--ink)}
  .goalcard{margin-top:24px;border:1px solid var(--line);border-radius:14px;background:var(--surface);overflow:hidden}
  .goalcard .obj{padding:16px 20px;font-size:15.5px;line-height:1.45} .goalcard .obj .l{display:block;font:700 9px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--goal);margin-bottom:7px} .goalcard .obj b{font-weight:680}
  .crit{display:flex;gap:12px;align-items:baseline;padding:11px 20px;border-top:1px solid var(--line);font-size:14px}
  .crit .cs{flex:none;width:74px;font:700 10px/1.3 var(--mono);letter-spacing:.04em;text-transform:uppercase;padding-top:1px}
  .crit .cs.met{color:var(--accent)} .crit .cs.short{color:var(--warn)} .crit .cs.risk{color:var(--bad)}
  .crit .ct{color:var(--ink)} .crit .ct .m{color:var(--ink-soft);font:500 12.5px/1.4 var(--mono)}
  .grouplbl{font:600 11px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-soft);margin:26px 0 10px}
  .tiles{display:grid;grid-template-columns:repeat(2,1fr);gap:1px;background:var(--line);border:1px solid var(--line);border-radius:14px;overflow:hidden}
  .tile{background:var(--surface);padding:15px 16px} .tile .k{font:500 11px/1 var(--mono);letter-spacing:.04em;text-transform:uppercase;color:var(--ink-soft)}
  .tile .v{font-size:22px;font-weight:680;margin-top:8px} .tile .d{font:500 12px/1.2 var(--sans);margin-top:5px;color:var(--ink-soft)} .up{color:var(--accent)} .down{color:var(--bad)} .flat{color:var(--warn)}
  .seclabel{font:600 12px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-soft);margin:34px 0 14px}
  .runs{border:1px solid var(--line);border-radius:12px;overflow:hidden;background:var(--surface)}
  .run{display:flex;align-items:center;gap:14px;padding:11px 16px;border-top:1px solid var(--line);font:500 13px/1 var(--mono);color:var(--ink-soft)}
  .run:first-child{border-top:none} .run.flag{background:var(--warn-soft)} .run .id{color:var(--ink);font-weight:700;width:40px}
  .run .st{display:inline-flex;align-items:center;gap:6px;width:92px} .run .st.ok{color:var(--accent)} .run .st.warn{color:var(--warn)}
  .run .st .d{width:6px;height:6px;border-radius:50%;background:currentColor} .run .col{width:74px} .run .col b{color:var(--ink)} .run .note{color:var(--warn);font:500 12px/1 var(--sans)} .run .ago{margin-left:auto;color:var(--line-strong)}
  .entry{background:var(--surface);border:1px solid var(--line);border-left:3px solid var(--line-strong);border-radius:12px;padding:18px 20px;margin-bottom:14px}
  .entry.monitor{border-left-color:var(--warn)} .entry.agent{border-left-color:var(--accent)} .entry.user{border-left-color:var(--user);background:var(--user-soft)} .entry.open{border-left-color:var(--bad)} .entry.note{border-left-color:var(--line-strong)}
  .ehead{display:flex;align-items:center;gap:9px;margin-bottom:9px;flex-wrap:wrap}
  .tag{font:700 10px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;padding:4px 7px;border-radius:5px}
  .tag.monitor{background:var(--warn-soft);color:var(--warn)} .tag.agent{background:var(--accent-soft);color:var(--accent)} .tag.user{background:var(--user-soft);color:var(--user)} .tag.open{background:var(--bad-soft);color:var(--bad)} .tag.note{background:#efeee9;color:var(--ink-soft)}
  .kind{font:700 9px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;padding:4px 7px;border-radius:5px;border:1px solid}
  .kind.bug{color:var(--bad);border-color:var(--bad-soft);background:#fdf3f2} .kind.goal{color:var(--goal);border-color:var(--goal-soft);background:#faf4fb}
  .etitle{font-weight:620;font-size:15px} .when{margin-left:auto;font:500 12px/1 var(--mono);color:var(--ink-soft)}
  .entry p{margin:0;font-size:14.5px} .entry p+p{margin-top:9px} .entry .meta{margin-top:11px;font:500 12px/1.4 var(--mono);color:var(--ink-soft)} .resolved{margin-top:10px;font:600 12.5px/1.4 var(--sans);color:var(--accent)}
  .archive{border:1px solid var(--line);border-radius:12px;background:var(--surface);overflow:hidden}
  .arow{display:flex;gap:14px;align-items:center;padding:14px 18px;border-top:1px solid var(--line);font-size:14px;color:var(--ink-soft)} .arow:first-child{border-top:none} .arow b{color:var(--ink)} .arow .n{margin-left:auto;font:500 12px/1 var(--mono)}
  footer{margin-top:44px;padding-top:18px;border-top:1px solid var(--line);font:500 12px/1.4 var(--mono);color:var(--ink-soft)}
</style>
</head>
<body><div class="wrap">

  <div class="top">
    <div><div class="crumb">workflow · log</div><h1><!-- WORKFLOW NAME --></h1></div>
    <!-- TWO VERDICTS. Bug: did it run right (ok|warn|bad). Goal: is it hitting success criteria (ok|warn|bad). -->
    <div class="verdicts">
      <div class="pill ok"><span class="lbl">Bug</span><span class="dot"></span>Bug-free</div>
      <div class="pill ok"><span class="lbl">Goal</span><span class="dot"></span>On-target</div>
    </div>
  </div>
  <div class="chips">
    <span class="chip">Type <b><!-- primary type --></b></span>
    <span class="chip">Oversight <b><!-- oversight_mode --></b></span>
    <span class="chip">Last run <b>—</b></span>
  </div>

  <!-- THE GOAL: objective + success criteria from soul.md, each with status (met|short|risk).
       The Goal verdict above is measured against these. Keep the Workflow Profile prose here too. -->
  <div class="goalcard">
    <div class="obj"><span class="l">What this workflow is for</span><!-- one-line objective from soul.md --></div>
    <div class="crit"><span class="cs met">✓ Met</span><span class="ct"><!-- success criterion --> <span class="m">— evidence/metric</span></span></div>
    <!-- one .crit row per success criterion; cs = met | short | risk -->
  </div>

  <!-- SIGNAL TILES grouped by verdict. Read every number from planning/metrics.json,
       db/metrics_history.jsonl, scores/evaluation/. Never invent. -->
  <p class="grouplbl">● Bug — operational health</p>
  <div class="tiles">
    <div class="tile"><div class="k">—</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>
  <p class="grouplbl">● Goal — success criteria</p>
  <div class="tiles">
    <div class="tile"><div class="k">—</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>

  <p class="seclabel">Recent runs</p>
  <div class="runs"><!-- one .run row per recent run; add .flag + a .note when something stands out --></div>

  <p class="seclabel">Latest — newest first</p>
  <!-- LOG ENTRIES: newest first -->
  <!-- Insert each new entry card immediately below this anchor. Monitor/Open-finding/Decision carry a
       <span class="kind bug">Bug</span> or <span class="kind goal">Goal</span> chip. Card kinds:
       <div class="entry monitor"><div class="ehead"><span class="tag monitor">Monitor</span><span class="kind bug">Bug</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry agent"><div class="ehead"><span class="tag agent">Agent · hardened</span><span class="kind bug">Bug</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p><p class="resolved">Resolved YYYY-MM-DD — how.</p></div>
       <div class="entry open" id="of-YYYY-MM-DD-slug"><div class="ehead"><span class="tag open">Open finding</span><span class="kind goal">Goal</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry user"><div class="ehead"><span class="tag user">User rule · authoritative</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry note"><div class="ehead"><span class="tag note">Note</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       Close an open finding by editing its card to add: <p class="resolved">Resolved YYYY-MM-DD — how.</p> -->

  <p class="seclabel" style="margin-top:36px">Archive</p>
  <div class="archive"><!-- one .arow per monthly archive file once you start rolling entries off --></div>

  <footer>generated by the workflow agent · newest first · bug + goal verdicts · older detail archived monthly</footer>

</div></body>
</html>
```
