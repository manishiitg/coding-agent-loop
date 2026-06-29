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

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Call `get_reference_doc(kind="html-output")` for the style baseline, and copy the **Starter HTML skeleton** at the bottom of this doc for the exact structure and polish. Top to bottom the document reads: **two verdicts → status headline → the goal → signal tiles → cost/time readout → recent runs → newest-first timeline → archive**.

The Pulse log is opened in a narrow right panel by default. Design it **mobile-first**:
the base CSS must work at 360-480px with stacked rows, no overlapping metadata, no
desktop-only tables, and long workflow names/ids allowed to wrap. Add desktop/tablet
enhancements with `@media (min-width: ...)`; do not make desktop the default and patch
mobile as an afterthought.

### The status headline (the 1-second read)

Directly under the verdicts, one `.status` banner carries a **single plain sentence** — the workflow's one-sentence verdict headline (there is no separate verdict file — this banner is the source of truth) — so a user knows "am I OK?" without parsing pills or scrolling. Its `ok|warn|bad` class tracks the **worse** of the two verdicts, and its `.when` shows the run + how long ago. Keep it honest both ways: on a clean, on-target run say so plainly ("Healthy and on-target."); on a regression lead with what's wrong ("Goal drifting — eval 0.78, under 0.90 target for 3 runs."). Never manufacture concern to fill it.

### Freshness — every status says which run it's "as of"

A verdict, a goal-criterion status, or a tile can silently go stale if no recent run measured it. So **stamp the run each status reflects**: the verdict pills carry a small `run #N`, each goal-criterion `.m` line ends with `· run #N`, and the status banner's `.when` shows the run + age. A 4-runs-old "Met" must read as 4-runs-old, not as current truth — this is how the reader tells a live verdict from a stale one.

### Two verdicts: Bug and Goal

Every workflow is judged on two independent axes, and the header shows **both** as separate pills — never collapse them into a single "health":

- **Bug** — did it *run correctly*? Errors, skipped steps, missing/empty artifacts, regressions vs the last run. A bug is fixed by **hardening**. Operational, roughly binary.
- **Goal** — is it *achieving its success criteria*? Eval scores and outcome metrics vs `soul.md`, trending over runs. A goal gap is fixed by **refining or replanning**. Continuous.

They are orthogonal: a run can be **Bug: broken** (a step silently skipped) while **Goal: on-target**, or **Bug: clean** while **Goal: short** (it runs perfectly but produces output that misses the point). You need both lenses — operational monitoring can't see a goal gap, and outcome metrics can't see a skipped step. **Health gates goal:** a run that wasn't operationally clean produces no trustworthy goal signal, so never judge the goal on a broken run.

Tag each **Monitor**, **Open finding**, and **Decision** entry with the axis it belongs to — a small **Bug** or **Goal** chip — so the timeline is filterable and the fix path is obvious (Bug → harden, Goal → refine/replan).

### The goal card

Directly under the verdicts, show **what the workflow is for**: the one-line objective plus the success criteria from `soul.md`, each with a live status — **Met / Short / At risk** — and the metric or evidence behind that status. This is what the **Goal** verdict is measured against; without it the verdict is opaque. Keep it current as criteria are met or slip. (The `/auto-improve` setup seeds this from `soul.md` when it bootstraps the goal.)

The goal card **reads from `soul.md`** — it does not replace it. `soul.md` stays Markdown (it's parsed for objective/success-criteria); **do not create a `soul.html`** or convert it. This Pulse log is the only HTML document; soul.md is its Markdown source.

### Signal tiles — grouped by verdict

Render metrics as readable tiles (value + movement in words: `eval 0.78 ▶ target 0.90`, `cost ¢19 ▲ from ¢12`, `wall 4m12s · LLM 2m08s`), grouped into **Bug tiles** (did it run: tests executed, last-run status, runtime), **Goal tiles** (is it achieving: eval scores vs target, outcome metrics vs success criteria), and **Cost/time tiles** (what the run spent: total cost/tokens, wall/LLM/tool time, top-cost step/agent, slowest step/agent). Read every number from `planning/metrics.json`, `db/metrics_history.jsonl`, `scores/evaluation/`, cost ledgers under `costs/`, and timing summaries under `runs/<run_folder>/logs/<step-id>/execution/` — the deterministic source of truth. Never fabricate a value or a trend, and never use charts.

### Cost/time readout — one compact operational report per run

Every run row needs the top-level total, and the latest timeline entry or a compact Note should carry the breakdown when there is enough evidence. The goal is a useful CEO/operator read, not a ledger dump.

Use this shape:

- **Total:** cost, tokens, wall time, LLM time, tool time, and evidence path.
- **By plan step:** step id/title, configured tier/model, observed provider/model, cost/tokens,
  wall/LLM/tool time, LLM calls, tool calls.
- **By agent/sub-agent when available:** parent step, agent/sub-agent id/name, model, cost/tokens,
  elapsed time. For `todo_task`, group child agents under the parent plan step.
- **By paid tool when relevant:** provider/model/tool, quantity, estimated/actual cost.

If evidence is missing, say `missing cost evidence`, `missing timing evidence`, or `unpriced provider`; do not estimate. This section is report-only. Do not imply Pulse changed model tiers, prompts, schedules, or agent allocation.

### Newest on top — always

New entries go at the **top** of the timeline, not appended at the bottom. The file carries a stable anchor comment `<!-- LOG ENTRIES: newest first -->` directly below the header/tiles; insert each new entry immediately after it with `diff_patch_workspace_file`. Never reorder or rewrite existing entries except to close out an open finding (below). **Always read the existing file first** so you continue its style and don't duplicate entries.

### Entry kinds

Each entry is a small card: a date, a kind tag, a one-line title, and a short prose body (2–4 sentences, plain language — explain *what* and *why*, link the evidence file or changelog entry when relevant). Use these kinds:

- **Run** — a one-line row in the recent-runs strip: run id, status, key numbers (tests, eval, cost/tokens, wall time), the **backup result** (`backed up ✓ <commit/ref>`, `unchanged — already backed up`, or `backup ✗ <reason>`), and a short note only when something stands out. Routine runs stay terse; flag a run only when it regressed, the backup failed, cost/time evidence is missing, or one step/agent dominates spend/time.
- **Monitor** — a post-run observation: what changed in the output and the most likely cause, correlated against the plan changelog ("output regressed at run N; you tightened step X two runs earlier — likely cause").
- **Decision** — a change applied or proposed, with the one-line rationale and the file(s) touched. If it fixes an open finding, close that finding out (below).
- **Chief of Staff recommendation** — an org-level recommendation written by Chief of Staff / Org Pulse after reading workflow evidence against org goals. It should name the org goal/KPI target or `supporting/no explicit goal`, give an alignment verdict (`aligned`, `supporting`, `unaligned`, or `unknown-measurement`), cite evidence, state the gap, suggest a builder action, and describe the expected KPI/success-criteria impact. Treat it like an external **Open finding**: verify the cited evidence, then choose the normal builder path (Bug → `harden_workflow`, Goal/strategy → `replan_workflow_from_results` or a targeted builder edit, measurement gap → metric/eval/report fix, cost/ops → review/apply if safe). Do not assume it is correct or already applied; close it only after the builder decision is made.
- **User rule** — a constraint the user stated. Mark it clearly as authoritative ("USER RULE — authoritative") so future agents treat it as a hard constraint, never silently override it. This replaces the old `source: "user"` field — say it in words.
- **Note** — a freeform observation or watchpoint that explains weird runs ("staging UI is mid-redesign, expect selector churn through ~June 20 — not a workflow bug").
- **Open finding** — something wrong that is not yet fixed. Give it a short stable anchor id (e.g. `id="of-2026-06-07-screenshots"`) **only so a later Decision can mark it resolved** — that is the one place an id earns its keep. No other entry kind needs an id.

### Closing out an open finding

When a change fixes an open finding, edit that finding's card in place to add a resolved line — don't delete it, don't open a duplicate:

```
Resolved 2026-06-09 — added a non-empty-screenshot pre-validation rule to audit-finalizer.
```

Reference the finding by its anchor id (or, if it has none, by its date + title). This keeps the "what's still outstanding vs. what's been handled" view honest.

### Confirming a decision's outcome (did the change actually work?)

A Decision card records what a harden/replan applied and *why* — but a journal that only ever says "applied X" never proves the system is working. So a Decision that changed behaviour stays **unconfirmed** until a later run measures its effect, and then it gets **one** outcome stamp added in place (never a second one, never a new card):

```
<p class="outcome ok">Confirmed by run #43 — login-skip gone, eval 0.72 → 0.81 over 2 runs.</p>
<p class="outcome bad">No effect by run #44 — reopened as Goal finding of-2026-06-12-eval.</p>
<p class="outcome flat">Inconclusive — run #44 didn't exercise the changed path; still pending.</p>
```

- **ok** — the expected number moved the right way (cite before → after and the run).
- **bad** — it didn't help or regressed; say so plainly and open (or reopen) a finding for it. A change that quietly failed is worse than no change, so never hide it.
- **flat** — the run that fired didn't exercise the changed path (routing), so the decision is still pending; leave it unconfirmed.

So a Decision is checkable, **state the expected effect when you write it** ("expect login-skip to stop and eval to recover toward 0.85") — that's the bar the later run is judged against. The per-run monitor owns applying these stamps (below); don't stamp a decision on the same run that made it.

### Keep the active file small

The log must not grow without bound. When `builder/improve.html` passes roughly **800 lines, 60 KB, or 20 timeline entries**, move older **resolved** findings, superseded decisions, and routine run rows into a monthly archive `builder/improve-archive/YYYY-MM.html`, leaving a one-row entry in the Archive Index (date range, count, any still-unresolved ids). **Never archive** open findings, user rules, current notes, or the latest few entries — the active file should always answer "what's the state of this workflow right now and what still needs attention."

### Upgrading an old-format log (one-time, REQUIRED before appending)

An existing `builder/improve.html` is **old-format** — and must be upgraded, not appended to — if it has **any** of: a title like "Improvement Ledger"; `## Active Improvement Index` / `## Recent Entries` / `## Archive Index` headings; ```improve-decision``` fenced/`<script>` JSON blocks; `F-…` / `I-…` ids; its own ad-hoc CSS (`.summary` / `.badge` / `.stats`, system-ui body) instead of the skeleton's; **or an older skeleton that lacks `<meta name="viewport">`, mobile-first stacked `.status` / `.run` / `.entry` layouts, or `overflow-wrap:anywhere`; **or an `.etitle` rule missing `flex:1 1 auto` (the older skeleton whose entry titles collapse to one word per line beside the `.when` meta, leaving the card half-empty)**. Legacy `builder/improve.md` / `builder/review.md` also count.

**Do NOT append your new entry into the old structure** — that produces good content in a stale, off-brand shell. Instead, **rewrite the entire document to the Starter HTML skeleton above** as a one-time upgrade:

1. Read the old file in full.
2. Write the skeleton fresh: header + two verdict pills, the goal card (objective + success criteria from `soul.md`), the signal tiles, the recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, the archive section.
3. Carry every **unresolved finding** and **still-relevant recent decision/run** forward as timeline **cards** (newest first), dropping the `<script>` JSON blocks and the `F-`/`I-` ids — write readable prose, give an open finding a short anchor id only.
4. Delete any legacy `.md` (`execute_shell_command`) so nothing is duplicated.

After this one rewrite the file is in skeleton format; from then on you just prepend cards. The structured JSON schema and the dual `F-/I-` id system are retired.

### Starter HTML skeleton (copy this exactly)

`builder/improve.html` renders in a full sandboxed iframe — the same way reports render — so it supports real CSS, web fonts, and themes. There is no excuse for a plain or ugly log: match the polish below. When bootstrapping a new log, write this document verbatim, fill the header/profile, and leave the `<!-- LOG ENTRIES: newest first -->` anchor in place. On every later turn, insert new entry cards **immediately after that anchor** (newest on top). Keep the CSS block stable so the look stays consistent run to run.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title><!-- WORKFLOW NAME --> · pulse</title>
<style>
  :root{
    --bg:#f7f7f5;--surface:#fff;--surface-2:#fbfbfa;
    --ink:#191917;--ink-2:#57564f;--ink-3:#8a897f;
    --line:#eceae4;--line-2:#e0ded7;
    --ok:#3f7a4a;--ok-bg:#eaf3ea;--warn:#9a6a05;--warn-bg:#fbf2dd;--bad:#b0322b;--bad-bg:#fbe9e6;
    --goal:#7c4a90;--goal-bg:#f4ecf7;--user:#3a4a8f;--user-bg:#eceffb;
    --shadow:0 1px 2px rgba(20,20,18,.04),0 4px 16px -8px rgba(20,20,18,.10);
    --mono:"SF Mono",ui-monospace,"JetBrains Mono",Menlo,monospace;--sans:"Inter",-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;--r:14px;}
  /* Dark palette — the app injects data-theme="dark" on <html> when its theme is dark. Keep this block. */
  html[data-theme="dark"]{
    --bg:#0a0a0c;--surface:#15151a;--surface-2:#101014;
    --ink:#f1f0f4;--ink-2:#9b9ba6;--ink-3:#64646e;
    --line:#212128;--line-2:#2e2e37;
    --ok:#5fd08a;--ok-bg:#0f2419;--warn:#e6b450;--warn-bg:#241d0c;--bad:#f47e76;--bad-bg:#2a1412;
    --goal:#d3a0e6;--goal-bg:#231829;--user:#92a6f5;--user-bg:#141a32;
    --shadow:0 1px 0 rgba(255,255,255,.04) inset,0 1px 2px rgba(0,0,0,.45),0 10px 30px -14px rgba(0,0,0,.75);}
  html{color-scheme:light} html[data-theme="dark"]{color-scheme:dark}
  *{box-sizing:border-box}
  html,body{width:100%;max-width:100%;overflow-x:hidden}
  body{margin:0;background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:14px;line-height:1.5;-webkit-font-smoothing:antialiased;font-feature-settings:"cv02","cv03","ss01";font-variant-numeric:tabular-nums;overflow-wrap:anywhere}
  html[data-theme="dark"] body{background:radial-gradient(1100px 520px at 50% -8%, #17171e 0%, var(--bg) 58%) fixed}
  code{overflow-wrap:anywhere}
  .wrap{width:100%;max-width:820px;margin:0 auto;padding:16px 12px 56px}
  .top{display:block}
  .eyebrow{font:600 11px/1 var(--mono);letter-spacing:.14em;color:var(--ink-3);text-transform:uppercase}
  h1{font-size:24px;line-height:1.08;letter-spacing:-.01em;margin:8px 0 0;font-weight:660}
  .verdicts{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}
  .pill{display:inline-flex;align-items:center;gap:7px;font:650 12px/1 var(--sans);padding:8px 11px;border-radius:999px;border:1px solid transparent;max-width:100%}
  .pill .lbl{font:700 8.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;opacity:.65}
  .pill .as{font:540 10px/1 var(--mono);opacity:.55;margin-left:1px}
  .pill.ok{background:var(--ok-bg);color:var(--ok);border-color:color-mix(in srgb,var(--ok) 16%,transparent)}
  .pill.warn{background:var(--warn-bg);color:var(--warn);border-color:color-mix(in srgb,var(--warn) 16%,transparent)}
  .pill.bad{background:var(--bad-bg);color:var(--bad);border-color:color-mix(in srgb,var(--bad) 18%,transparent)}
  .dot{width:7px;height:7px;border-radius:50%;background:currentColor;box-shadow:0 0 0 3px color-mix(in srgb,currentColor 18%,transparent)}
  /* Status headline — the 1-second read; mirrors the monitor's one-sentence verdict. */
  .status{display:flex;align-items:flex-start;gap:10px;flex-wrap:wrap;margin:18px 0 0;padding:13px 14px;border-radius:13px;border:1px solid var(--line-2);background:var(--surface);box-shadow:var(--shadow);font-size:14px;font-weight:560}
  .status .ic{flex:none;width:9px;height:9px;border-radius:50%;background:currentColor;box-shadow:0 0 0 4px color-mix(in srgb,currentColor 15%,transparent)}
  .status.ok{color:var(--ok)} .status.warn{color:var(--warn)} .status.bad{color:var(--bad)}
  .status .txt{color:var(--ink);font-weight:580;min-width:0;flex:1 1 220px}.status .when{margin-left:19px;flex-basis:100%;font:540 11px/1.35 var(--mono);color:var(--ink-3);white-space:normal}
  .chips{display:flex;flex-wrap:wrap;gap:7px;margin-top:16px}
  .chip{font:520 12px/1 var(--sans);padding:6px 11px;border-radius:8px;background:var(--surface);border:1px solid var(--line-2);color:var(--ink-2)} .chip b{color:var(--ink);font-weight:600}
  .goalcard{margin-top:26px;border:1px solid var(--line-2);border-radius:var(--r);background:var(--surface);box-shadow:var(--shadow);overflow:hidden}
  .goalcard .obj{padding:15px 15px 14px;font-size:14px;line-height:1.5}.goalcard .obj .l{display:block;font:700 9px/1 var(--mono);letter-spacing:.12em;text-transform:uppercase;color:var(--goal);margin-bottom:9px}.goalcard .obj b{font-weight:670}
  .crit{display:block;padding:11px 15px;border-top:1px solid var(--line);font-size:13.5px}
  .crit .cs{display:inline-flex;margin-bottom:6px;font:700 9.5px/1.3 var(--mono);letter-spacing:.03em;text-transform:uppercase;padding-top:2px}
  .crit .cs.met{color:var(--ok)} .crit .cs.short{color:var(--warn)} .crit .cs.risk{color:var(--bad)}
  .crit .ct{color:var(--ink)} .crit .ct .m{display:block;margin-top:3px;color:var(--ink-3);font:520 12px/1.45 var(--mono)}
  .grouplbl{display:flex;align-items:center;gap:8px;font:650 11px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3);margin:30px 2px 12px} .grouplbl::after{content:"";flex:1;height:1px;background:var(--line)}
  .seclabel{font:650 11px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3);margin:34px 2px 14px}
  .tiles{display:grid;grid-template-columns:1fr;gap:10px}
  .tile{min-width:0;background:var(--surface);border:1px solid var(--line-2);border-radius:12px;padding:13px 14px;box-shadow:var(--shadow)}
  .tile .k{font:600 10.5px/1 var(--mono);letter-spacing:.05em;text-transform:uppercase;color:var(--ink-3)}
  .tile .v{font-size:25px;font-weight:680;letter-spacing:-.02em;margin-top:10px;line-height:1} .tile .d{font:540 12px/1.3 var(--sans);margin-top:7px;color:var(--ink-2)}
  .up{color:var(--ok)} .down{color:var(--bad)} .flat{color:var(--warn)}
  .runs{border:1px solid var(--line-2);border-radius:12px;overflow:hidden;background:var(--surface);box-shadow:var(--shadow)}
  .run{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:7px 10px;align-items:start;padding:12px 14px;border-top:1px solid var(--line);font:540 12px/1.35 var(--mono);color:var(--ink-2)}
  .run:first-child{border-top:none} .run.flag{background:color-mix(in srgb,var(--warn-bg) 60%,var(--surface))}
  .run .id{color:var(--ink);font-weight:680}.run .st{display:inline-flex;align-items:center;gap:6px}
  .run .st.ok{color:var(--ok)} .run .st.warn{color:var(--warn)} .run .st .d{width:5px;height:5px;border-radius:50%;background:currentColor}
  .run .col b{color:var(--ink);font-weight:620}.run .note{grid-column:1/-1;color:var(--warn);font:560 12px/1.35 var(--sans)}.run .ago{grid-column:1/-1;color:var(--ink-3)}
  .entry{position:relative;background:var(--surface);border:1px solid var(--line-2);border-radius:13px;padding:15px 14px 15px 18px;margin-bottom:12px;box-shadow:var(--shadow);min-width:0}
  .entry::before{content:"";position:absolute;left:0;top:14px;bottom:14px;width:3px;border-radius:3px;background:var(--line-2)}
  .entry.monitor::before{background:var(--warn)} .entry.agent::before{background:var(--ok)} .entry.user::before{background:var(--user)} .entry.open::before{background:var(--bad)} .entry.note::before{background:var(--ink-3)}
  .ehead{display:flex;align-items:center;gap:7px;margin-bottom:8px;flex-wrap:wrap}
  .tag{font:700 9.5px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;padding:4px 8px;border-radius:6px}
  .tag.monitor{background:var(--warn-bg);color:var(--warn)} .tag.agent{background:var(--ok-bg);color:var(--ok)} .tag.user{background:var(--user-bg);color:var(--user)} .tag.open{background:var(--bad-bg);color:var(--bad)} .tag.note{background:var(--surface-2);color:var(--ink-2);border:1px solid var(--line-2)}
  .kind{font:700 8.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;padding:4px 7px;border-radius:6px;border:1px solid}
  .kind.bug{color:var(--bad);border-color:color-mix(in srgb,var(--bad) 22%,transparent)} .kind.goal{color:var(--goal);border-color:color-mix(in srgb,var(--goal) 22%,transparent)}
  .etitle{font-weight:630;font-size:14px;line-height:1.25;letter-spacing:-.01em;flex:1 1 auto;min-width:0}.ehead>.when{margin-left:0;flex-basis:100%;font:540 11px/1.35 var(--mono);color:var(--ink-3)}
  .entry p{margin:0;font-size:13.5px;color:var(--ink);overflow-wrap:anywhere}.entry p+p{margin-top:8px}
  .entry .meta{margin-top:11px;padding-top:11px;border-top:1px solid var(--line);font:540 12px/1.5 var(--mono);color:var(--ink-3)} .entry .meta code{background:var(--surface-2);border:1px solid var(--line);border-radius:5px;padding:1px 6px;color:var(--ink-2)}
  .resolved{margin-top:11px;display:inline-flex;align-items:center;gap:7px;font:620 12.5px/1.4 var(--sans);color:var(--ok)} .resolved::before{content:"✓";font-size:11px;width:16px;height:16px;display:inline-flex;align-items:center;justify-content:center;border-radius:50%;background:var(--ok-bg)}
  /* Outcome stamp on a Decision card — did the change actually move the number, judged by a later run. */
  .outcome{margin-top:11px;display:inline-flex;align-items:flex-start;gap:7px;font:600 12.5px/1.45 var(--sans)}
  .outcome::before{flex:none;font-size:11px;width:16px;height:16px;margin-top:1px;display:inline-flex;align-items:center;justify-content:center;border-radius:50%}
  .outcome.ok{color:var(--ok)} .outcome.ok::before{content:"✓";background:var(--ok-bg)}
  .outcome.bad{color:var(--bad)} .outcome.bad::before{content:"✗";background:var(--bad-bg)}
  .outcome.flat{color:var(--warn)} .outcome.flat::before{content:"–";background:var(--warn-bg)}
  .archive{border:1px solid var(--line-2);border-radius:12px;background:var(--surface);overflow:hidden;box-shadow:var(--shadow)}
  .arow{display:block;padding:13px 14px;border-top:1px solid var(--line);font-size:13.5px;color:var(--ink-2)} .arow:first-child{border-top:none} .arow b{color:var(--ink);font-weight:620} .arow .n{display:block;margin-top:4px;font:540 11px/1.35 var(--mono);color:var(--ink-3)}
  footer{margin-top:42px;padding-top:18px;border-top:1px solid var(--line);font:540 11.5px/1.5 var(--mono);color:var(--ink-3)}
  @media (min-width:640px){
    body{font-size:15px}
    .wrap{padding:28px 26px 88px}
    .top{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;flex-wrap:wrap}
    h1{font-size:31px;line-height:1.05;letter-spacing:-.025em}
    .verdicts{margin-top:0}.pill{font-size:13px;padding:9px 14px 9px 12px}
    .status{align-items:center;gap:12px;margin-top:22px;padding:15px 19px;font-size:15.5px}.status .txt{flex:1 1 auto}.status .when{margin-left:auto;flex-basis:auto;white-space:nowrap;font-size:12px}
    .goalcard .obj{padding:18px 22px 17px;font-size:16px}.crit{display:flex;gap:13px;align-items:baseline;padding:12px 22px;font-size:14px}.crit .cs{flex:none;width:78px;margin-bottom:0}
    .tiles{grid-template-columns:repeat(2,minmax(0,1fr))}.tile{padding:15px 16px}
    .run{display:flex;align-items:center;gap:13px;padding:12px 16px;font-size:13px;line-height:1}.run .id{width:38px}.run .st{width:96px}.run .col{width:78px}.run .note{grid-column:auto}.run .ago{grid-column:auto;margin-left:auto}
    .entry{padding:17px 19px 17px 22px}.etitle{font-size:15px}.ehead>.when{margin-left:auto;flex-basis:auto;white-space:nowrap;font-size:12px}.entry p{font-size:14.5px}
    .arow{display:flex;gap:13px;align-items:center;padding:14px 18px;font-size:14px}.arow .n{display:block;margin-left:auto;margin-top:0;font-size:12px}
  }
</style>
</head>
<body><div class="wrap">

  <div class="top">
    <div><div class="eyebrow">workflow · pulse</div><h1><!-- WORKFLOW NAME --></h1></div>
    <!-- TWO VERDICTS. Bug: did it run right (ok|warn|bad). Goal: is it hitting success criteria (ok|warn|bad). -->
    <div class="verdicts">
      <!-- Each pill carries the run it's as-of so a stale verdict can't read as current truth. -->
      <div class="pill ok"><span class="lbl">Bug</span><span class="dot"></span>Bug-free<span class="as">run #—</span></div>
      <div class="pill warn"><span class="lbl">Goal</span><span class="dot"></span>Not yet measured</div>
    </div>
  </div>

  <!-- STATUS HEADLINE — the 1-second read. ONE plain sentence, the workflow's verdict headline (the
       source of truth — there is no separate file). Class ok|warn|bad tracks the worse of the two verdicts.
       On a clean, on-target run say so plainly; don't manufacture concern. -->
  <div class="status ok">
    <span class="ic"></span>
    <span class="txt"><!-- e.g. Healthy and on-target. --></span>
    <span class="when"><!-- run #— · — ago --></span>
  </div>

  <div class="chips">
    <span class="chip">Type <b><!-- primary type --></b></span>
    <span class="chip">Oversight <b><!-- oversight_mode --></b></span>
    <span class="chip">Last run <b>—</b></span>
  </div>

  <!-- THE GOAL: objective + success criteria from soul.md, each with status (met|short|risk).
       The Goal verdict above is measured against these. Keep the Workflow Profile prose nearby. -->
  <div class="goalcard">
    <div class="obj"><span class="l">What this workflow is for</span><!-- one-line objective from soul.md --></div>
    <div class="crit"><span class="cs short">↑ Short</span><span class="ct"><!-- success criterion --><span class="m">not yet measured — needs a run</span></span></div>
    <!-- one .crit row per success criterion; cs = met | short | risk.
         End each .m metric line with the run it's as-of so freshness is visible:
         <span class="m">eval 0.81 ▶ 0.90 target · run #41</span>. A criterion whose route this run
         didn't exercise is "not run this route" (cs short, neutral), never Short/At-risk. -->
  </div>

  <!-- SIGNAL TILES grouped by verdict. Read every number from planning/metrics.json,
       db/metrics_history.jsonl, scores/evaluation/, costs/, and timing summaries. Never invent. -->
  <div class="grouplbl">Bug · operational health</div>
  <div class="tiles">
    <div class="tile"><div class="k">—</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>
  <div class="grouplbl">Goal · success criteria</div>
  <div class="tiles">
    <div class="tile"><div class="k">—</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>
  <div class="grouplbl">Cost + time · latest run</div>
  <div class="tiles">
    <div class="tile"><div class="k">Cost</div><div class="v">—</div><div class="d">missing cost evidence</div></div>
    <div class="tile"><div class="k">Time</div><div class="v">—</div><div class="d">missing timing evidence</div></div>
    <!-- Keep this section compact. Good tile examples:
         Cost: "$0.27" / "1.2M tokens · top: score-companies $0.18"
         Time: "4m12s" / "LLM 2m08s · tools 51s · slowest: browser-agent 1m22s"
         Model mix: "high: opus · medium: sonnet" / "observed: claude-sonnet-4-6"
         Evidence: "costs/execution/group/date.json · runs/<run>/logs/<step>/execution/timing.json" -->
  </div>

  <div class="seclabel">Recent runs</div>
  <div class="runs"><!-- one .run row per recent run; include status, eval, cost/tokens, wall time, backup; add .flag + a .note when something stands out --></div>

  <div class="seclabel">Latest — newest first</div>
  <!-- LOG ENTRIES: newest first -->
  <!-- Insert each new entry card immediately below this anchor. Monitor/Open-finding/Decision carry a
       <span class="kind bug">Bug</span> or <span class="kind goal">Goal</span> chip. Card kinds:
       <div class="entry monitor"><div class="ehead"><span class="tag monitor">Monitor</span><span class="kind bug">Bug</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry agent"><div class="ehead"><span class="tag agent">Agent · hardened</span><span class="kind bug">Bug</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p><p class="resolved">Resolved YYYY-MM-DD — how.</p></div>
       <div class="entry open" id="of-YYYY-MM-DD-slug"><div class="ehead"><span class="tag open">Open finding</span><span class="kind goal">Goal</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry user"><div class="ehead"><span class="tag user">User rule · authoritative</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry note"><div class="ehead"><span class="tag note">Note</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       Close an open finding by editing its card to add: <p class="resolved">Resolved YYYY-MM-DD — how.</p>
       Confirm a Decision worked (or didn't) by editing its card to add ONE outcome stamp once a later run measures it:
       <p class="outcome ok">Confirmed by run #43 — login-skip gone, eval 0.72 → 0.81 over 2 runs.</p>
       <p class="outcome bad">No effect by run #44 — reopened as <span class="kind goal">Goal</span> finding of-YYYY-MM-DD-slug.</p>
       <p class="outcome flat">Inconclusive — run #44 didn't exercise the changed path; still pending. -->

  <div class="seclabel">Archive</div>
  <div class="archive"><!-- one .arow per monthly archive file once you start rolling entries off --></div>

  <footer>generated by the workflow agent · newest first · bug + goal verdicts · archived monthly</footer>

</div></body>
</html>
```
