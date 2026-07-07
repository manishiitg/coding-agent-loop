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

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Call `get_reference_doc(kind="html-output")` for the style baseline. When creating or upgrading the file, also load `get_reference_doc(kind="review-improve-log-skeleton")` and copy that **Starter HTML skeleton** for the exact structure and polish. Top to bottom the document reads: **two verdicts → status headline → "what matters now" widget brief → the goal → color-coded signal tiles → cost/time readout → activity filters → recent runs → newest-first card timeline → archive**. The first screen should read like a daily operator dashboard, not a raw ledger.

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

### Plain-language card contract

The user reads this page to understand what happened without opening raw logs. Every visible card must be readable in 10 seconds:

- Start with one plain-language takeaway: `<p class="takeaway">...</p>`. Say the outcome first, not the internal step name.
- Then use short labelled detail: **What happened**, **Why it matters**, **Next**, and **Evidence**. Keep each field to one simple sentence when possible.
- Put technical paths, run ids, SQL names, model ids, and raw hashes in **Evidence** or `.meta`, not in the first sentence.
- Avoid compressed ledger language: no long semicolon chains, no unexplained abbreviations, and no dense strings like `regression(high)+low_signal`.
- Prefer user words: "The workflow did not send the email" is better than "delivery notify scope mismatch"; "Goal is short for the third clean run" is better than "low_signal persists".
- If a card cannot be understood without opening raw files, rewrite it before saving.

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

Each entry is a small card: a date, a kind tag, optional classification chips, a one-line title, and a short prose body (2–4 sentences, plain language — explain *what* and *why*, link the evidence file or changelog entry when relevant). The first body line must be a `<p class="takeaway">...</p>` that a non-technical operator can understand before reading the details. Use these kinds:

- **Run** — a one-line row in the recent-runs strip: run id, status, key numbers (tests, eval, cost/tokens, wall time), the **backup result** (`backed up ✓ <commit/ref>`, `unchanged — already backed up`, or `backup ✗ <reason>`), and a short note only when something stands out. Routine runs stay terse; flag a run only when it regressed, the backup failed, cost/time evidence is missing, or one step/agent dominates spend/time.
- **Monitor** — a post-run observation: what changed in the output and the most likely cause, correlated against the plan changelog ("output regressed at run N; you tightened step X two runs earlier — likely cause").
- **Artifact Review** — a report-only Pulse/review entry: changelog range inspected, Artifact Sync Cursor before/after, steps inspected, clean/no-pending result or drift findings, and the recommended next owner. Do not present this entry as a fix that already happened; Pulse does not call harden/replan from this item.
- **Decision** — a change applied or proposed, with the one-line rationale and the file(s) touched. If it fixes an open finding, close that finding out (below). Auto-improve decisions use `<div class="entry decision">` with tag text `Decision - Auto-improve - Applied` or `Decision - Auto-improve - Proposed`; use `<div class="entry decision major">` for material replans, report/eval changes that alter user-facing success measurement, cadence/scope changes, or any change the user should notice.
- **Advisor opportunity** — a proposal-only Auto Improve entry for an out-of-plan idea the current workflow has not considered but an expert operator would raise because it could materially advance the goal. It should be grounded in `soul.md`, run/eval/report evidence, market/process reasoning, or a clearly stated assumption; never present speculation as fact. Record it as `Decision - Auto-improve - Proposed` with the `Goal` chip and `Advisor idea` work label, and include why it is outside the current plan, what evidence/assumption supports it, the expected upside, and the risk/next decision. Do not auto-apply it from the advisor scan alone.
- **Chief of Staff recommendation** — an org-level recommendation written by Chief of Staff / Org Pulse after reading workflow evidence against org goals. It should name the org goal/KPI target or `supporting/no explicit goal`, give an alignment verdict (`aligned`, `supporting`, `unaligned`, or `unknown-measurement`), cite evidence, state the gap, suggest a builder action, and describe the expected KPI/success-criteria impact. Treat it like an external **Open finding**: verify the cited evidence, then choose the normal builder path (Bug → `harden_workflow`, Goal/strategy → `replan_workflow_from_results` or a targeted builder edit, measurement gap → eval/report fix, cost/ops → review/apply if safe). Do not assume it is correct or already applied; close it only after the builder decision is made.
- **Human input requested** — a durable question whenever Pulse, Auto Improve, or Chief of Staff needs the user to decide, clarify, approve, provide credentials, or choose between options. Do not ask only in email/chat. Create or refresh the request with `create_human_input_request`; it is stored in this workflow's `db/db.sqlite` table `report_human_inputs`. The visible `builder/improve.html` card may display the question, why it matters, options, evidence, and answer status, but it must not be the source of truth. When a later pass uses the answer, call `mark_human_input_consumed` with the outcome summary instead of editing the SQLite row directly.
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
- A `<p class="takeaway">...</p>` before the grid that says the decision in one user-readable sentence.
- `.decisiongrid` rows for the fixed fields: **Why now**, **Evidence**, **Change**, **Expected impact**, **Files touched**, and **Risk / gap**. Omit a row only when it truly does not apply; do not bury these fields in prose. Each field should be one short sentence; if a field needs raw technical detail, put the human meaning first and the raw path/id second.

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
  <p class="takeaway">We changed the plan because verified replies stayed below target for three clean runs.</p>
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

**Do NOT append your new entry into the old structure** — that produces good content in a stale, off-brand shell. Instead, **rewrite the entire document using `get_reference_doc(kind="review-improve-log-skeleton")`** as a one-time upgrade:

1. Read the old file in full.
2. Load `get_reference_doc(kind="review-improve-log-skeleton")` and write the skeleton fresh: header + two verdict pills, status headline, the "What matters now" brief, the goal card (objective + success criteria from `soul.md`), the signal tiles, filter bar, the recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, the archive section.
3. Carry every **unresolved finding** and **still-relevant recent decision/run** forward as timeline **cards** (newest first), dropping the `<script>` JSON blocks and the `F-`/`I-` ids — write readable prose, give an open finding a short anchor id only.
4. Delete any legacy `.md` (`execute_shell_command`) so nothing is duplicated.

After this one rewrite the file is in skeleton format; from then on you just prepend cards. The structured JSON schema and the dual `F-/I-` id system are retired.

### Starter HTML skeleton

The full copy-paste HTML skeleton lives in `get_reference_doc(kind="review-improve-log-skeleton")`. Load it when creating `builder/improve.html` or doing the required one-time old-format upgrade. Keep this reference doc focused on log semantics; the skeleton reference keeps the CSS, filter script, and card examples in one place without bloating every normal review/improve prompt.
