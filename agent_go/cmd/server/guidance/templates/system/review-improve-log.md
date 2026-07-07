## Workflow log conventions

Canonical conventions shared by every `/review-*` and `/improve-*` skill, the post-run monitor, and any chat-driven fix that touches the log. Load this once; the individual skills point here instead of restating it.

### One log: `builder/improve.html`

The workflow keeps a **single durable log** — `builder/improve.html` — the workflow's journal and the user's primary window into it. Everything the user should be able to see later goes here, in one place:

- **applied or proposed changes** (what `/improve-*`, harden/replan, and chat fixes did, and why),
- **review findings** (what `/review-*` flagged — recommendations, REVIEW = recommend, do NOT apply),
- **run notes and the recent-run log** (what happened on recent runs),
- **monitor observations** (post-run regressions / drift the monitor caught),
- **Artifact Review reports** (plan-change artifact drift cursor, clean/no-pending result, or drift findings),
- **human input requests** (questions or choices the user must answer, with status and fallback),
- **user rules** (authoritative constraints the user stated).

Do not create a separate review document. All review findings, monitor notes, decisions, human input requests, user rules, Artifact Review entries, and run notes belong in `builder/improve.html`.

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Call `get_reference_doc(kind="html-output")` for the style baseline, and copy the **Starter HTML skeleton** at the bottom of this doc for the exact structure and polish. Top to bottom the document reads: **two verdicts → status headline → "what matters now" widget brief → the goal → color-coded signal tiles → cost/time readout → activity filters → recent runs → newest-first card timeline → archive**. The first screen should read like a daily operator dashboard, not a raw ledger.

The Pulse log is opened in a narrow right panel by default. Design it **mobile-first**:
the base CSS must work at 360-480px with stacked rows, no overlapping metadata, no
desktop-only tables, and long workflow names/ids allowed to wrap. Add desktop/tablet
enhancements with `@media (min-width: ...)`; do not make desktop the default and patch
mobile as an afterthought.

### The status headline (the 1-second read)

Directly under the verdicts, one `.status` banner carries a **single plain sentence** — the workflow's one-sentence verdict headline (there is no separate verdict file — this banner is the source of truth) — so a user knows "am I OK?" without parsing pills or scrolling. Its `ok|warn|bad` class tracks the **worse** of the two verdicts, and its `.when` shows the run + how long ago. Keep it honest both ways: on a clean, on-target run say so plainly ("Healthy and on-target."); on a regression lead with what's wrong ("Goal drifting — eval 0.78, under 0.90 target for 3 runs."). Never manufacture concern to fill it.

### Freshness — every status says which run it's "as of"

A verdict, a goal-criterion status, or a tile can silently go stale if no recent run measured it. So **stamp the run each status reflects**: the verdict pills carry a small `run #N`, each goal-criterion `.m` line ends with `· run #N`, and the status banner's `.when` shows the run + age. A 4-runs-old "Met" must read as 4-runs-old, not as current truth — this is how the reader tells a live verdict from a stale one.

### What matters now

Directly below the chips, include one `.brief` section with 2-4 short cells. Use it to explain the current operating picture in human terms: latest result, main risk, next useful action, and evidence confidence. Keep each cell to one or two sentences. This is not another timeline; it is the executive/operator summary that tells the user what to pay attention to before they scroll. Prefer widget cells and chips over paragraphs; if a cell needs more than two short sentences, split it into another tile or move the detail to the timeline.

### Filterable activity

Every recent-run row and every timeline entry must be filterable. Add:

- `data-date="YYYY-MM-DD"` — the actual event/run date in local workflow time when known; if only a run folder date exists, use that date.
- `data-kind="run|monitor|artifact|decision|advisor|cos|input|open|user|note"` — the primary activity type.

The built-in filter bar searches both recent runs and timeline cards by exact date, kind, and free text. Do not remove the static filter script from the skeleton. It is UI behavior, not a legacy JSON data block.

### Two verdicts: Bug and Goal

Every workflow is judged on two independent axes, and the header shows **both** as separate pills — never collapse them into a single "health":

- **Bug** — did it *run correctly*? Errors, skipped steps, missing/empty artifacts, regressions vs the last run. A bug is fixed by **hardening**. Operational, roughly binary.
- **Goal** — is it *achieving its success criteria*? Eval scores and run evidence vs `soul.md`, trending over runs. A goal gap is fixed by **refining or replanning**. Continuous.

They are orthogonal: a run can be **Bug: broken** (a step silently skipped) while **Goal: on-target**, or **Bug: clean** while **Goal: short** (it runs perfectly but produces output that misses the point). You need both lenses: operational monitoring catches run failures, while eval and output review catch goal gaps. **Health gates goal:** a run that wasn't operationally clean produces no trustworthy goal signal, so never judge the goal on a broken run.

Tag each **Monitor**, **Open finding**, **Artifact Review**, and **Decision** entry with the axis it belongs to — a small **Bug** or **Goal** chip when applicable — so the timeline is filterable and the fix path is obvious (Bug → harden, Goal → refine/replan). Also add an action label chip when work was done, proposed, or a user answer is needed (`Bug fix`, `Improvement`, `Advisor idea`, `Artifact drift`, `Report fix`, `Eval fix`, `Cost/time`, `Backup/publish`, `Needs input`, `Manual`). Auto-improve decisions must be visually distinct from Pulse harden notes: use the dedicated Decision card classes below, not a generic Note or Agent card.

### The goal card

Directly under the verdicts, show **what the workflow is for**: the one-line objective plus the success criteria from `soul.md`, each with a live status — **Met / Short / At risk** — and the eval/run evidence behind that status. This is what the **Goal** verdict is measured against; without it the verdict is opaque. Keep it current as criteria are met or slip. (The `/auto-improve` setup seeds this from `soul.md` when it bootstraps the goal.)

The goal card **reads from `soul.md`** — it does not replace it. `soul.md` stays Markdown (it's parsed for objective/success-criteria); **do not create a `soul.html`** or convert it. This Pulse log is the only HTML document; soul.md is its Markdown source.

### Signal tiles — grouped by verdict

Render readable, color-coded signal tiles (value + movement in words: `eval 0.78 -> target 0.90`, `cost 19c -> from 12c`, `wall 4m12s · LLM 2m08s`), grouped into **Bug tiles** (did it run: tests executed, last-run status, runtime), **Goal tiles** (is it achieving: eval scores and output checks vs success criteria), and **Cost/time tiles** (what the run spent: total cost/tokens, wall/LLM/tool time, top-cost step/agent, slowest step/agent). Use `.tile.ok`, `.tile.warn`, `.tile.bad`, `.tile.info`, `.tile.goal`, or `.tile.cost` to make the first screen scannable. Read every number from eval reports, run outputs/logs, cost ledgers under `costs/`, and timing summaries under `runs/<run_folder>/logs/<step-id>/execution/` — the deterministic source of truth. Never fabricate a value or a trend, and never use charts.

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

Each entry is a small card: a date, a kind tag, optional classification chips, a one-line title, and a short prose body (2–4 sentences, plain language — explain *what* and *why*, link the evidence file or changelog entry when relevant). Use these kinds:

- **Run** — a one-line row in the recent-runs strip: run id, status, key numbers (tests, eval, cost/tokens, wall time), the **backup result** (`backed up ✓ <commit/ref>`, `unchanged — already backed up`, or `backup ✗ <reason>`), and a short note only when something stands out. Routine runs stay terse; flag a run only when it regressed, the backup failed, cost/time evidence is missing, or one step/agent dominates spend/time.
- **Monitor** — a post-run observation: what changed in the output and the most likely cause, correlated against the plan changelog ("output regressed at run N; you tightened step X two runs earlier — likely cause").
- **Artifact Review** — a report-only Pulse/review entry: changelog range inspected, Artifact Sync Cursor before/after, steps inspected, clean/no-pending result or drift findings, and the recommended next owner. Do not present this entry as a fix that already happened; Pulse does not call harden/replan from this item.
- **Decision** — a change applied or proposed, with the one-line rationale and the file(s) touched. If it fixes an open finding, close that finding out (below). Auto-improve decisions use `<div class="entry decision">` with tag text `Decision - Auto-improve - Applied` or `Decision - Auto-improve - Proposed`; use `<div class="entry decision major">` for material replans, report/eval changes that alter user-facing success measurement, cadence/scope changes, or any change the user should notice.
- **Advisor opportunity** — a proposal-only Auto Improve entry for an out-of-plan idea the current workflow has not considered but an expert operator would raise because it could materially advance the goal. It should be grounded in `soul.md`, run/eval/report evidence, market/process reasoning, or a clearly stated assumption; never present speculation as fact. Record it as `Decision - Auto-improve - Proposed` with the `Goal` chip and `Advisor idea` work label, and include why it is outside the current plan, what evidence/assumption supports it, the expected upside, and the risk/next decision. Do not auto-apply it from the advisor scan alone.
- **Chief of Staff recommendation** — an org-level recommendation written by Chief of Staff / Org Pulse after reading workflow evidence against org goals. It should name the org goal/KPI target or `supporting/no explicit goal`, give an alignment verdict (`aligned`, `supporting`, `unaligned`, or `unknown-measurement`), cite evidence, state the gap, suggest a builder action, and describe the expected KPI/success-criteria impact. Treat it like an external **Open finding**: verify the cited evidence, then choose the normal builder path (Bug → `harden_workflow`, Goal/strategy → `replan_workflow_from_results` or a targeted builder edit, measurement gap → eval/report fix, cost/ops → review/apply if safe). Do not assume it is correct or already applied; close it only after the builder decision is made.
- **Human input requested** — a durable question card whenever a Pulse/Auto Improve/Builder notification asks the user to decide, clarify, approve, provide credentials, or choose between options. Do not ask only in email/chat. Use `class="entry input"`, `data-kind="input"`, `data-question-id="<stable id>"`, `data-status="open|answered|dismissed|expired"`, and optional `data-default-action`. The visible card must include **Question**, **Why it matters**, **Options / expected answer**, **Default if no answer**, **Evidence**, and **Asked at**. Keep the title short enough for the timeline. If the same question is still open, update the existing card instead of duplicating it. When the user answers or the default action is taken, edit the same card with a resolved/outcome line and change `data-status`.
- **User rule** — a constraint the user stated. Mark it clearly as authoritative ("USER RULE — authoritative") so future agents treat it as a hard constraint, never silently override it. This replaces the old `source: "user"` field — say it in words.
- **Note** — a freeform observation or watchpoint that explains weird runs ("staging UI is mid-redesign, expect selector churn through ~June 20 — not a workflow bug").
- **Open finding** — something wrong that is not yet fixed. Give it a short stable anchor id (e.g. `id="of-2026-06-07-screenshots"`) **only so a later Decision can mark it resolved** — that is the one place an id earns its keep. No other entry kind needs an id.

### Chief of Staff Recommendation Handoff

Chief of Staff / Org Pulse and workflow Pulse communicate through structured cards in this log. A CoS card must be an entry element with:

- `class` including `cos-rec`
- `data-cos-rec-id="<stable id>"` (stable across days; do not regenerate for the same goal/gap)
- `data-goal-id="<org goal id or supporting>"`
- `data-status="<status>"`
- `data-priority="high|medium|low"`
- `data-suggested-action="harden_workflow|replan_workflow_from_results|eval_report_measurement_fix|manual_review|no_action_watchpoint|queued_auto_improve"`
- optional `data-impact`, `data-effort`, and `data-status-*` attributes written by the marker tool

Lifecycle statuses:

- `proposed` — Org Pulse suggested it; workflow Pulse has not triaged it yet.
- `accepted` — workflow Pulse agrees the recommendation is valid but has not routed it yet.
- `queued_auto_improve` — strategic/Goal work that scheduled Auto Improve should consider.
- `in_progress` — a builder/auto-improve action is currently addressing it.
- `needs_evidence` — the recommendation may be right but lacks enough evidence or measurement.
- `done` — workflow Pulse or Auto Improve handled it and cited the confirming evidence.
- `dismissed` — workflow Pulse rejected it with a reason.
- `blocked` — valid, but waiting on user input, credentials, external data, or another dependency.

Workflow Pulse and Auto Improve must use `mark_cos_recommendation_status` to change these statuses instead of hand-editing lifecycle attributes. They may still add a visible Decision/Monitor entry that explains the action, but the status marker is the machine-readable reply that Org Pulse reads next time.

Example:

```html
<article class="entry cos-rec open-finding" id="cos-2026-07-03-reply-rate"
  data-date="2026-07-03"
  data-kind="cos"
  data-cos-rec-id="cos-2026-07-03-reply-rate"
  data-goal-id="goal-qualified-pipeline"
  data-status="proposed"
  data-priority="high"
  data-impact="high"
  data-effort="medium"
  data-suggested-action="replan_workflow_from_results">
  <div class="ehead">
    <span class="tag">Chief of Staff recommendation</span>
    <span class="kind goal">Goal</span>
    <span class="worklabel improvement">Improvement</span>
    <span class="etitle">Retarget outreach around verified replies</span>
    <span class="when">2026-07-03 · Org Pulse</span>
  </div>
  <p><b>Goal/KPI:</b> Qualified pipeline · reply-rate target. <b>Evidence:</b> reports/dashboard.html · evaluation/latest.json. <b>Gap:</b> reply rate is capped despite clean runs. <b>Expected impact:</b> improve reply-rate evidence before increasing volume.</p>
</article>
```

### Classification chips

Use two different chip families so the user can scan what happened:

- **Verdict chip**: `<span class="kind bug">Bug</span>` or `<span class="kind goal">Goal</span>` answers which verdict lane the entry belongs to. Monitor, Open finding, Decision, and Artifact Review entries should carry one when applicable.
- **Action label chip**: `<span class="worklabel ...">...</span>` answers what kind of work/fix this was. Use at most two per entry, immediately after the verdict chip.

Canonical action labels:

- `worklabel bugfix` → `Bug fix`: Pulse/harden changed prompts/config/guards/validation/code shape to make the workflow run correctly.
- `worklabel improvement` → `Improvement`: Auto Improve or a builder change improved strategy, plan quality, success criteria alignment, cadence, or user-facing usefulness.
- `worklabel advisor` → `Advisor idea`: Auto Improve proposed an out-of-plan expert recommendation or unconventional opportunity that could help the goal but needs user choice or stronger evidence before changing the plan.
- `worklabel artifact` → `Artifact drift`: Artifact Review found or resolved plan-change drift in reports, evals, KB/learnings, saved code, db wiring, or generated HTML.
- `worklabel report` → `Report fix`: report dashboard/query/HTML/data-binding repair.
- `worklabel eval` → `Eval fix`: evaluation rubric, eval wiring, route scoping, or score evidence repair.
- `worklabel cost` → `Cost/time`: LLM/model/cost/time telemetry observation or repair.
- `worklabel backup` → `Backup/publish`: backup or publish repair/status issue.
- `worklabel input` → `Needs input`: the workflow is waiting for a user answer or decision.
- `worklabel manual` → `Manual`: user-requested/manual builder action.

Do not over-label routine clean runs. If a change spans categories, choose the primary action label and add one secondary label only when it helps scanning (for example `Bug fix` + `Report fix`, or `Advisor idea` + `Improvement` when a proposal is both out-of-plan and directly tied to strategy).

### Closing out an open finding

When a change fixes an open finding, edit that finding's card in place to add a resolved line — don't delete it, don't open a duplicate:

```
Resolved 2026-06-09 — added a non-empty-screenshot pre-validation rule to audit-finalizer.
```

Reference the finding by its anchor id (or, if it has none, by its date + title). This keeps the "what's still outstanding vs. what's been handled" view honest.

### Decision cards (clear action and why)

A Decision card is the visual proof that an agent took or proposed an action. It should never read like a routine note. Use:

- `<div class="entry decision">` for normal applied/proposed auto-improve decisions.
- `<div class="entry decision major">` when the decision changes plan strategy, report/eval measurement, workflow cadence/scope, user-facing dashboard interpretation, or materially affects cost/quality/risk.
- `.tag.decision` with one of these exact labels: `Decision - Auto-improve - Applied`, `Decision - Auto-improve - Proposed`, `Decision - Pulse harden`, or `Decision - Manual`.
- A verdict chip plus an action label chip. Examples: Pulse harden uses `<span class="kind bug">Bug</span><span class="worklabel bugfix">Bug fix</span>`; an auto-improve replan uses `<span class="kind goal">Goal</span><span class="worklabel improvement">Improvement</span>`; an out-of-plan advisor proposal uses `<span class="kind goal">Goal</span><span class="worklabel advisor">Advisor idea</span>`; a report dashboard repair uses `<span class="kind bug">Bug</span><span class="worklabel report">Report fix</span>`.
- `.decisiongrid` rows for the fixed fields: **Why now**, **Evidence**, **Change**, **Expected impact**, **Files touched**, and **Risk / gap**. Omit a row only when it truly does not apply; do not bury these fields in prose.

Example:

```html
<div class="entry decision major">
  <div class="ehead">
    <span class="tag decision">Decision - Auto-improve - Applied</span>
    <span class="kind goal">Goal</span>
    <span class="worklabel improvement">Improvement</span>
    <span class="etitle">Replanned lead-scoring around verified replies</span>
    <span class="when">2026-07-02 · scheduled improve</span>
  </div>
  <div class="decisiongrid">
    <div><b>Why now</b><span>Reply rate stayed below the 8% target for three clean runs.</span></div>
    <div><b>Evidence</b><span>evaluation/latest.json · db/reports/dashboard.html · run #43</span></div>
    <div><b>Change</b><span>Reordered enrichment before outreach and added a verified-reply gate.</span></div>
    <div><b>Expected impact</b><span>Raise reply-rate evidence toward the success criterion without increasing send volume.</span></div>
    <div><b>Files touched</b><span>planning/plan.json · planning/step_config.json · builder/improve.html</span></div>
    <div><b>Risk / gap</b><span>Needs two more clean runs before confirming impact.</span></div>
  </div>
</div>
```

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

An existing `builder/improve.html` is **old-format** — and must be upgraded, not appended to — if it has **any** of:

- a title like "Improvement Ledger";
- `## Active Improvement Index` / `## Recent Entries` / `## Archive Index` headings;
- ```improve-decision``` fenced/`<script>` JSON blocks;
- `F-…` / `I-…` ids;
- legacy Markdown improve logs;
- its own ad-hoc CSS (`.summary` / `.badge` / `.stats`, system-ui body) instead of the skeleton's;
- no `<meta name="viewport">`;
- missing mobile-first stacked `.status` / `.run` / `.entry` layouts or prose-safe overflow rules;
- an `.etitle` rule missing `flex:1 1 auto`, or an `.ehead > .when` rule that keeps `margin-left:auto` / `white-space:nowrap` in the base mobile CSS. That older skeleton collapses entry titles and body text into narrow columns beside timestamp metadata, leaving the card half-empty in the right panel.
- any recent-runs table/flex/grid whose date/status/type/age metadata can shrink into one-character columns. This usually comes from global `overflow-wrap:anywhere` on `body`, `td`, or metadata cells. Rewrite those rows as stacked/mobile-first cards or keep metadata/chips non-wrapping (`white-space:nowrap; overflow-wrap:normal; word-break:normal`) while only prose/evidence fields use `overflow-wrap:anywhere`.
- any recent-runs desktop layout that puts the long `.note`/evidence text beside date/status/type/age metadata. The note must sit on a full-width second row so the run list stays readable in both the right panel and a wide browser.
- missing `.filters` UI or missing `data-date` / `data-kind` attributes on recent-run rows and timeline entries. Add the filter bar and backfill dates/kinds from visible dates, run folders, entry labels, or best available evidence.
- missing `.worklabel` CSS/action-label examples. Current logs need action chips such as `Bug fix`, `Improvement`, `Advisor idea`, `Artifact drift`, `Report fix`, `Eval fix`, `Cost/time`, `Backup/publish`, `Needs input`, and `Manual` so the user can scan what kind of work happened.
- a text-heavy first screen with no widget brief, no color-coded tiles, or recent runs rendered as a dense table. Upgrade it to the richer dashboard shell before appending new entries.

**Do NOT append your new entry into the old structure** — that produces good content in a stale, off-brand shell. Instead, **rewrite the entire document to the Starter HTML skeleton above** as a one-time upgrade:

1. Read the old file in full.
2. Write the skeleton fresh: header + two verdict pills, status headline, the "What matters now" brief, the goal card (objective + success criteria from `soul.md`), the signal tiles, filter bar, the recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, the archive section.
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
    --ok:#247a58;--ok-bg:#e4f7ed;--warn:#a45f00;--warn-bg:#fff0cf;--bad:#bd3445;--bad-bg:#ffe3e8;
    --goal:#7c4dd8;--goal-bg:#f0e9ff;--decision:#0d7584;--decision-bg:#e3f7f8;--major:#c43d79;--major-bg:#ffe4f0;--user:#2c70c9;--user-bg:#e7f0ff;--teal:#168477;--teal-bg:#dff7f2;--amber:#b65c00;--amber-bg:#fff0d6;
    --shadow:0 1px 2px rgba(20,20,18,.04),0 4px 16px -8px rgba(20,20,18,.10);
    --mono:"SF Mono",ui-monospace,"JetBrains Mono",Menlo,monospace;--sans:"Inter",-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;--r:14px;}
  /* Dark palette — the app injects data-theme="dark" on <html> when its theme is dark. Keep this block. */
  html[data-theme="dark"]{
    --bg:#0a0a0c;--surface:#15151a;--surface-2:#101014;
    --ink:#f1f0f4;--ink-2:#9b9ba6;--ink-3:#64646e;
    --line:#212128;--line-2:#2e2e37;
    --ok:#69dfa0;--ok-bg:#10291d;--warn:#f0ba59;--warn-bg:#2c210e;--bad:#ff8794;--bad-bg:#32151b;
    --goal:#c4a7ff;--goal-bg:#201632;--decision:#77d5e4;--decision-bg:#102a30;--major:#ff8abc;--major-bg:#321421;--user:#82b8ff;--user-bg:#10213b;--teal:#5ee4d2;--teal-bg:#0d2a27;--amber:#f5b45f;--amber-bg:#2d1f0c;
    --shadow:0 1px 0 rgba(255,255,255,.04) inset,0 1px 2px rgba(0,0,0,.45),0 10px 30px -14px rgba(0,0,0,.75);}
  html{color-scheme:light} html[data-theme="dark"]{color-scheme:dark}
  *{box-sizing:border-box}
  html,body{width:100%;max-width:100%;overflow-x:hidden}
  body{margin:0;background:var(--bg);color:var(--ink);font-family:var(--sans);font-size:14px;line-height:1.5;-webkit-font-smoothing:antialiased;font-feature-settings:"cv02","cv03","ss01";font-variant-numeric:tabular-nums;overflow-wrap:normal;word-break:normal}
  html[data-theme="dark"] body{background:radial-gradient(1100px 520px at 50% -8%, #17171e 0%, var(--bg) 58%) fixed}
  code,.status .txt,.briefitem p,.crit .ct,.tile .d,.entry p,.entry .meta,.decisiongrid span,.arow,footer{overflow-wrap:anywhere}
  .wrap{width:100%;max-width:820px;margin:0 auto;padding:16px 12px 56px}
  .top{display:block}
  .eyebrow{font:600 11px/1 var(--mono);letter-spacing:.14em;color:var(--ink-3);text-transform:uppercase}
  h1{font-size:24px;line-height:1.08;letter-spacing:-.01em;margin:8px 0 0;font-weight:660}
  .verdicts{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}
  .pill{display:inline-flex;align-items:center;gap:7px;font:650 12px/1 var(--sans);padding:8px 11px;border-radius:999px;border:1px solid transparent;max-width:100%;white-space:nowrap;overflow-wrap:normal;word-break:normal}
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
  .chip{font:520 12px/1 var(--sans);padding:6px 11px;border-radius:8px;background:var(--surface);border:1px solid var(--line-2);color:var(--ink-2);white-space:nowrap;overflow-wrap:normal;word-break:normal} .chip b{color:var(--ink);font-weight:600}
  .brief{margin-top:16px;border:1px solid var(--line-2);border-radius:var(--r);background:linear-gradient(180deg,color-mix(in srgb,var(--surface-2) 72%,var(--surface)),var(--surface));box-shadow:var(--shadow);padding:14px}
  .brief-h{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:10px;font:700 10.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--ink-3)}
  .brief-h b{font:600 11px/1.2 var(--mono);letter-spacing:0;text-transform:none;color:var(--ink-2);white-space:nowrap}
  .briefgrid{display:grid;grid-template-columns:1fr;gap:9px}
  .briefitem{min-width:0;padding:10px 11px;border:1px solid var(--line);border-radius:10px;background:color-mix(in srgb,var(--surface) 86%,var(--surface-2))}
  .briefitem .k{font:700 9.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3);margin-bottom:6px}
  .briefitem p{margin:0;font:540 13px/1.45 var(--sans);color:var(--ink)}
  .briefitem.ok{border-color:color-mix(in srgb,var(--ok) 18%,var(--line));background:color-mix(in srgb,var(--ok-bg) 22%,var(--surface))}
  .briefitem.warn{border-color:color-mix(in srgb,var(--warn) 20%,var(--line));background:color-mix(in srgb,var(--warn-bg) 26%,var(--surface))}
  .briefitem.bad{border-color:color-mix(in srgb,var(--bad) 20%,var(--line));background:color-mix(in srgb,var(--bad-bg) 24%,var(--surface))}
  .filters{display:grid;grid-template-columns:1fr;gap:9px;margin:28px 0 0;padding:12px;border:1px solid var(--line-2);border-radius:12px;background:var(--surface);box-shadow:var(--shadow)}
  .filters label{display:grid;gap:6px;font:700 9.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3)}
  .filters input,.filters select{width:100%;min-height:34px;border:1px solid var(--line-2);border-radius:9px;background:var(--surface-2);color:var(--ink);font:540 13px/1.2 var(--sans);padding:7px 9px}
  .filters button{min-height:34px;border:1px solid var(--line-2);border-radius:9px;background:var(--surface-2);color:var(--ink-2);font:650 12px/1 var(--sans);padding:7px 11px;cursor:pointer}
  .filters button:hover{border-color:var(--ink-3);color:var(--ink)}
  .filtercount{align-self:end;font:600 11px/1.35 var(--mono);color:var(--ink-3)}
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
  .tile.ok{border-color:color-mix(in srgb,var(--ok) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--ok-bg) 40%,var(--surface)),var(--surface))}
  .tile.warn{border-color:color-mix(in srgb,var(--warn) 26%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--warn-bg) 42%,var(--surface)),var(--surface))}
  .tile.bad{border-color:color-mix(in srgb,var(--bad) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--bad-bg) 40%,var(--surface)),var(--surface))}
  .tile.info{border-color:color-mix(in srgb,var(--user) 22%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--user-bg) 40%,var(--surface)),var(--surface))}
  .tile.goal{border-color:color-mix(in srgb,var(--goal) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--goal-bg) 42%,var(--surface)),var(--surface))}
  .tile.cost{border-color:color-mix(in srgb,var(--amber) 24%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--amber-bg) 38%,var(--surface)),var(--surface))}
  .tile .k{font:600 10.5px/1 var(--mono);letter-spacing:.05em;text-transform:uppercase;color:var(--ink-3)}
  .tile .v{font-size:25px;font-weight:680;letter-spacing:-.02em;margin-top:10px;line-height:1} .tile .d{font:540 12px/1.3 var(--sans);margin-top:7px;color:var(--ink-2)}
  .up{color:var(--ok)} .down{color:var(--bad)} .flat{color:var(--warn)}
  .runs{border:1px solid var(--line-2);border-radius:12px;overflow:hidden;background:var(--surface);box-shadow:var(--shadow)}
  .run{display:grid;grid-template-columns:1fr;gap:7px 10px;align-items:start;padding:12px 14px;border-top:1px solid var(--line);font:540 12px/1.35 var(--mono);color:var(--ink-2)}
  .run:first-child{border-top:none} .run.flag{background:color-mix(in srgb,var(--warn-bg) 60%,var(--surface))}.run[hidden],.entry[hidden]{display:none!important}
  .run .id{color:var(--ink);font-weight:680}.run .st{display:inline-flex;align-items:center;gap:6px}
  .run .st.ok{color:var(--ok)} .run .st.warn{color:var(--warn)} .run .st .d{width:5px;height:5px;border-radius:50%;background:currentColor}
  .run .id,.run .st,.run .col,.run .ago,.tag,.kind,.worklabel,.status .when,.ehead>.when{white-space:nowrap;overflow-wrap:normal;word-break:normal}
  .run .col b{color:var(--ink);font-weight:620}.run .note{grid-column:1/-1;color:var(--ink-2);font:560 12px/1.4 var(--sans);min-width:0;overflow-wrap:anywhere}.run.flag .note{color:var(--warn)}.run .ago{grid-column:1/-1;color:var(--ink-3)}
  .entry{position:relative;background:var(--surface);border:1px solid var(--line-2);border-radius:13px;padding:15px 14px 15px 18px;margin-bottom:12px;box-shadow:var(--shadow);min-width:0}
  .entry::before{content:"";position:absolute;left:0;top:14px;bottom:14px;width:3px;border-radius:3px;background:var(--line-2)}
  .entry.monitor::before{background:var(--warn)} .entry.agent::before{background:var(--ok)} .entry.decision::before{background:var(--decision)} .entry.decision.major::before{background:var(--major);width:4px} .entry.user::before{background:var(--user)} .entry.input::before{background:var(--user)} .entry.open::before{background:var(--bad)} .entry.note::before{background:var(--ink-3)}
  .entry.decision{border-color:color-mix(in srgb,var(--decision) 28%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--decision-bg) 46%,var(--surface)),var(--surface) 72%)}
  .entry.decision.major{border-color:color-mix(in srgb,var(--major) 38%,var(--line-2));background:linear-gradient(180deg,color-mix(in srgb,var(--major-bg) 62%,var(--surface)),var(--surface) 76%);box-shadow:0 0 0 1px color-mix(in srgb,var(--major) 15%,transparent),var(--shadow)}
  .ehead{display:flex;align-items:center;gap:7px;margin-bottom:8px;flex-wrap:wrap}
  .tag{font:700 9.5px/1 var(--mono);letter-spacing:.06em;text-transform:uppercase;padding:4px 8px;border-radius:6px}
  .tag.monitor{background:var(--warn-bg);color:var(--warn)} .tag.agent{background:var(--ok-bg);color:var(--ok)} .tag.decision{background:var(--decision-bg);color:var(--decision);border:1px solid color-mix(in srgb,var(--decision) 22%,transparent)} .entry.major .tag.decision{background:var(--major-bg);color:var(--major);border-color:color-mix(in srgb,var(--major) 25%,transparent)} .tag.user,.tag.input{background:var(--user-bg);color:var(--user)} .tag.open{background:var(--bad-bg);color:var(--bad)} .tag.note{background:var(--surface-2);color:var(--ink-2);border:1px solid var(--line-2)}
  .kind{font:700 8.5px/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;padding:4px 7px;border-radius:6px;border:1px solid}
  .kind.bug{color:var(--bad);border-color:color-mix(in srgb,var(--bad) 22%,transparent)} .kind.goal{color:var(--goal);border-color:color-mix(in srgb,var(--goal) 22%,transparent)}
  .worklabel{font:700 8.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;padding:4px 7px;border-radius:999px;background:var(--surface-2);border:1px solid var(--line-2);color:var(--ink-2)}
  .worklabel.bugfix{color:var(--bad);background:var(--bad-bg);border-color:color-mix(in srgb,var(--bad) 20%,transparent)} .worklabel.improvement{color:var(--goal);background:var(--goal-bg);border-color:color-mix(in srgb,var(--goal) 20%,transparent)} .worklabel.advisor{color:var(--major);background:var(--major-bg);border-color:color-mix(in srgb,var(--major) 22%,transparent)} .worklabel.artifact{color:var(--decision);background:var(--decision-bg);border-color:color-mix(in srgb,var(--decision) 20%,transparent)} .worklabel.report,.worklabel.eval{color:var(--warn);background:var(--warn-bg);border-color:color-mix(in srgb,var(--warn) 20%,transparent)} .worklabel.cost{color:var(--ink-2);background:var(--surface-2);border-color:var(--line-2)} .worklabel.backup{color:var(--ok);background:var(--ok-bg);border-color:color-mix(in srgb,var(--ok) 20%,transparent)} .worklabel.input,.worklabel.manual{color:var(--user);background:var(--user-bg);border-color:color-mix(in srgb,var(--user) 20%,transparent)}
  .etitle{font-weight:630;font-size:14px;line-height:1.25;letter-spacing:-.01em;flex:1 1 auto;min-width:0}.ehead>.when{margin-left:0;flex-basis:100%;font:540 11px/1.35 var(--mono);color:var(--ink-3)}
  .entry p{margin:0;font-size:13.5px;color:var(--ink)}.entry p+p{margin-top:8px}
  .entry .meta{margin-top:11px;padding-top:11px;border-top:1px solid var(--line);font:540 12px/1.5 var(--mono);color:var(--ink-3)} .entry .meta code{background:var(--surface-2);border:1px solid var(--line);border-radius:5px;padding:1px 6px;color:var(--ink-2)}
  .decisiongrid{display:grid;grid-template-columns:1fr;gap:8px;margin-top:11px}.decisiongrid>div{padding:9px 10px;border:1px solid color-mix(in srgb,var(--decision) 15%,var(--line));border-radius:10px;background:color-mix(in srgb,var(--surface) 88%,var(--decision-bg))}.entry.major .decisiongrid>div{border-color:color-mix(in srgb,var(--major) 18%,var(--line));background:color-mix(in srgb,var(--surface) 86%,var(--major-bg))}.decisiongrid b{display:block;margin-bottom:4px;font:700 9.5px/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3)}.decisiongrid span{display:block;color:var(--ink);font-size:13px;line-height:1.4}
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
    .brief{padding:16px}.briefgrid{grid-template-columns:repeat(2,minmax(0,1fr))}
    .filters{grid-template-columns:160px 150px minmax(160px,1fr) auto auto;align-items:end;padding:13px 14px}.filtercount{justify-self:end;white-space:nowrap}
    .goalcard .obj{padding:18px 22px 17px;font-size:16px}.crit{display:flex;gap:13px;align-items:baseline;padding:12px 22px;font-size:14px}.crit .cs{flex:none;width:78px;margin-bottom:0}
    .tiles{grid-template-columns:repeat(2,minmax(0,1fr))}.tile{padding:15px 16px}
    .run{display:grid;grid-template-columns:auto auto auto minmax(0,1fr) auto;gap:8px 14px;align-items:center;padding:12px 16px;font-size:13px;line-height:1.25}.run .id{grid-column:1;grid-row:1;min-width:44px}.run .st{grid-column:2;grid-row:1}.run .col{grid-row:1;min-width:78px}.run .note{grid-column:1/-1;grid-row:2;margin-top:4px;font-size:13px;line-height:1.45}.run .ago{grid-column:5;grid-row:1;justify-self:end;margin-left:0}
    .entry{padding:17px 19px 17px 22px}.etitle{font-size:15px}.ehead>.when{margin-left:auto;flex-basis:auto;white-space:nowrap;font-size:12px}.entry p{font-size:14.5px}
    .decisiongrid{grid-template-columns:repeat(2,minmax(0,1fr))}.decisiongrid span{font-size:13.5px}
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

  <!-- WHAT MATTERS NOW — 2-4 short operator-summary cells. Keep this brief; details belong in Recent runs/timeline. -->
  <div class="brief">
    <div class="brief-h">What matters now <b><!-- as of run #— --></b></div>
    <div class="briefgrid">
      <div class="briefitem ok"><div class="k">Latest result</div><p><!-- one short sentence --></p></div>
      <div class="briefitem warn"><div class="k">Main risk</div><p><!-- one short sentence --></p></div>
      <div class="briefitem"><div class="k">Next useful action</div><p><!-- one short sentence --></p></div>
      <div class="briefitem"><div class="k">Evidence confidence</div><p><!-- one short sentence --></p></div>
    </div>
  </div>

  <!-- THE GOAL: objective + success criteria from soul.md, each with status (met|short|risk).
       The Goal verdict above is measured against these. Keep the Workflow Profile prose nearby. -->
  <div class="goalcard">
    <div class="obj"><span class="l">What this workflow is for</span><!-- one-line objective from soul.md --></div>
    <div class="crit"><span class="cs short">↑ Short</span><span class="ct"><!-- success criterion --><span class="m">not yet measured — needs a run</span></span></div>
    <!-- one .crit row per success criterion; cs = met | short | risk.
         End each .m evidence line with the run it's as-of so freshness is visible:
         <span class="m">eval 0.81 ▶ 0.90 target · run #41</span>. A criterion whose route this run
         didn't exercise is "not run this route" (cs short, neutral), never Short/At-risk. -->
  </div>

  <!-- SIGNAL TILES grouped by verdict. Read every number from eval reports,
       run outputs/logs, costs/, and timing summaries. Never invent. -->
  <div class="grouplbl">Bug · operational health</div>
  <div class="tiles">
    <div class="tile ok"><div class="k">Run status</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>
  <div class="grouplbl">Goal · success criteria</div>
  <div class="tiles">
    <div class="tile goal"><div class="k">Goal signal</div><div class="v">—</div><div class="d">no runs yet</div></div>
  </div>
  <div class="grouplbl">Cost + time · latest run</div>
  <div class="tiles">
    <div class="tile cost"><div class="k">Cost</div><div class="v">—</div><div class="d">missing cost evidence</div></div>
    <div class="tile info"><div class="k">Time</div><div class="v">—</div><div class="d">missing timing evidence</div></div>
    <!-- Keep this section compact. Good tile examples:
         Cost: "$0.27" / "1.2M tokens · top: score-companies $0.18"
         Time: "4m12s" / "LLM 2m08s · tools 51s · slowest: browser-agent 1m22s"
         Model mix: "high: opus · medium: sonnet" / "observed: claude-sonnet-4-6"
         Evidence: "costs/execution/group/date.json · runs/<run>/logs/<step>/execution/timing.json" -->
  </div>

  <div class="filters" aria-label="Activity filters">
    <label>Date <input id="filter-date" type="date"></label>
    <label>Kind <select id="filter-kind">
      <option value="all">All</option>
      <option value="run">Run</option>
      <option value="monitor">Monitor</option>
      <option value="artifact">Artifact</option>
      <option value="decision">Decision</option>
      <option value="advisor">Advisor</option>
      <option value="cos">Chief of Staff</option>
      <option value="input">Needs input</option>
      <option value="open">Open finding</option>
      <option value="user">User rule</option>
      <option value="note">Note</option>
    </select></label>
    <label>Search <input id="filter-search" type="search" placeholder="Text in runs or entries"></label>
    <button id="filter-clear" type="button">Reset</button>
    <div id="filter-count" class="filtercount">0 items</div>
  </div>

  <div class="seclabel">Recent runs</div>
  <div class="runs"><!-- one .run row per recent run. Metadata stays in row 1; the prose/evidence .note is row 2/full width.
       Example:
       <div class="run flag" data-date="2026-07-04" data-kind="run"><span class="id">07-04</span><span class="st warn"><span class="d"></span>completed</span><span class="col">measure</span><span class="col"><b>Δ7d</b> -2</span><span class="ago">just now</span><span class="note">measure ran clean; regression still open; cost $2.02; backed up ✓ 3b1b357</span></div> --></div>

  <div class="seclabel">Latest — newest first</div>
  <!-- LOG ENTRIES: newest first -->
  <!-- Insert each new entry card immediately below this anchor. Monitor/Open-finding/Decision/Artifact Review carry a
       <span class="kind bug">Bug</span> or <span class="kind goal">Goal</span> verdict chip when applicable, plus a
       <span class="worklabel bugfix">Bug fix</span>, <span class="worklabel improvement">Improvement</span>, <span class="worklabel advisor">Advisor idea</span>, <span class="worklabel artifact">Artifact drift</span>, <span class="worklabel report">Report fix</span>, <span class="worklabel eval">Eval fix</span>, <span class="worklabel cost">Cost/time</span>, <span class="worklabel backup">Backup/publish</span>, <span class="worklabel input">Needs input</span>, or <span class="worklabel manual">Manual</span> action chip when work was done/proposed. Card kinds:
       <div class="entry monitor" data-date="YYYY-MM-DD" data-kind="monitor"><div class="ehead"><span class="tag monitor">Monitor</span><span class="kind bug">Bug</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry agent" data-date="YYYY-MM-DD" data-kind="decision"><div class="ehead"><span class="tag agent">Agent · hardened</span><span class="kind bug">Bug</span><span class="worklabel bugfix">Bug fix</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p><p class="resolved">Resolved YYYY-MM-DD — how.</p></div>
       <div class="entry decision major" data-date="YYYY-MM-DD" data-kind="decision"><div class="ehead"><span class="tag decision">Decision - Auto-improve - Applied</span><span class="kind goal">Goal</span><span class="worklabel improvement">Improvement</span><span class="etitle">…</span><span class="when">…</span></div><div class="decisiongrid"><div><b>Why now</b><span>…</span></div><div><b>Evidence</b><span>…</span></div><div><b>Change</b><span>…</span></div><div><b>Expected impact</b><span>…</span></div><div><b>Files touched</b><span>…</span></div><div><b>Risk / gap</b><span>…</span></div></div></div>
       <div class="entry decision major" data-date="YYYY-MM-DD" data-kind="advisor"><div class="ehead"><span class="tag decision">Decision - Auto-improve - Proposed</span><span class="kind goal">Goal</span><span class="worklabel advisor">Advisor idea</span><span class="etitle">…</span><span class="when">…</span></div><div class="decisiongrid"><div><b>Why now</b><span>…</span></div><div><b>Evidence</b><span>…</span></div><div><b>Change</b><span>Proposal only — out-of-plan idea and next decision.</span></div><div><b>Expected impact</b><span>…</span></div><div><b>Files touched</b><span>builder/improve.html only</span></div><div><b>Risk / gap</b><span>…</span></div></div></div>
       <div class="entry open" id="of-YYYY-MM-DD-slug" data-date="YYYY-MM-DD" data-kind="open"><div class="ehead"><span class="tag open">Open finding</span><span class="kind goal">Goal</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry input" data-date="YYYY-MM-DD" data-kind="input" data-question-id="input-YYYY-MM-DD-slug" data-status="open" data-default-action="…"><div class="ehead"><span class="tag input">Human input requested</span><span class="worklabel input">Needs input</span><span class="etitle">…</span><span class="when">…</span></div><div class="decisiongrid"><div><b>Question</b><span>…</span></div><div><b>Why it matters</b><span>…</span></div><div><b>Options / expected answer</b><span>…</span></div><div><b>Default if no answer</b><span>…</span></div><div><b>Evidence</b><span>…</span></div><div><b>Asked at</b><span>…</span></div></div></div>
       <div class="entry user" data-date="YYYY-MM-DD" data-kind="user"><div class="ehead"><span class="tag user">User rule · authoritative</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       <div class="entry note" data-date="YYYY-MM-DD" data-kind="note"><div class="ehead"><span class="tag note">Note</span><span class="etitle">…</span><span class="when">…</span></div><p>…</p></div>
       Close an open finding by editing its card to add: <p class="resolved">Resolved YYYY-MM-DD — how.</p>
       Confirm a Decision worked (or didn't) by editing its card to add ONE outcome stamp once a later run measures it:
       <p class="outcome ok">Confirmed by run #43 — login-skip gone, eval 0.72 → 0.81 over 2 runs.</p>
       <p class="outcome bad">No effect by run #44 — reopened as <span class="kind goal">Goal</span> finding of-YYYY-MM-DD-slug.</p>
       <p class="outcome flat">Inconclusive — run #44 didn't exercise the changed path; still pending. -->

  <div class="seclabel">Archive</div>
  <div class="archive"><!-- one .arow per monthly archive file once you start rolling entries off --></div>

  <footer>generated by the workflow agent · newest first · bug + goal verdicts · archived monthly</footer>

<script>
(function(){
  var dateInput = document.getElementById('filter-date');
  var kindInput = document.getElementById('filter-kind');
  var searchInput = document.getElementById('filter-search');
  var clearButton = document.getElementById('filter-clear');
  var count = document.getElementById('filter-count');
  function norm(value){ return (value || '').toString().trim().toLowerCase(); }
  function items(){ return Array.prototype.slice.call(document.querySelectorAll('.run[data-date], .entry[data-date]')); }
  function apply(){
    var date = dateInput ? dateInput.value : '';
    var kind = kindInput ? kindInput.value : 'all';
    var query = norm(searchInput ? searchInput.value : '');
    var total = 0;
    var shown = 0;
    items().forEach(function(el){
      total += 1;
      var okDate = !date || el.getAttribute('data-date') === date;
      var okKind = kind === 'all' || el.getAttribute('data-kind') === kind;
      var okText = !query || norm(el.textContent).indexOf(query) !== -1;
      var ok = okDate && okKind && okText;
      el.hidden = !ok;
      if (ok) shown += 1;
    });
    if (count) count.textContent = (date || kind !== 'all' || query) ? (shown + ' / ' + total + ' shown') : (total + ' items');
  }
  [dateInput, kindInput, searchInput].forEach(function(el){ if (el) el.addEventListener('input', apply); });
  if (clearButton) clearButton.addEventListener('click', function(){
    if (dateInput) dateInput.value = '';
    if (kindInput) kindInput.value = 'all';
    if (searchInput) searchInput.value = '';
    apply();
  });
  apply();
})();
</script>
</div></body>
</html>
```
