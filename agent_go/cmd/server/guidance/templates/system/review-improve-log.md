## Workflow log conventions

Canonical conventions shared by every `/review-*` and `/improve-*` skill, the post-run monitor, and any chat-driven fix that touches the log. Load this once; the individual skills point here instead of restating it.

### Reviewer/writer boundary

Pulse specialists investigate; the active Workshop parent or Pulse Fixer writes.
This boundary applies to Bug Review, Artifact Review, Learning Health,
Knowledgebase Health, DB Health, Eval Health, Report Health, Cost/LLM/Time,
LLM/Ops Review, plan review, and both Goal Advisor reviewers.

- A specialist is strictly read-only. It must not edit workflow files, update
  `builder/improve.html`, create or consume human-input requests, mark module
  state, or launch another maintenance reviewer.
- A specialist may read only the relevant semantic regions of
  `builder/improve.html`: matching open findings, recent entries for its module,
  current verdicts, answered outcomes, and an active Advisor experiment when
  relevant. It must not inspect CSS, restyle the page, migrate markup, load
  `review-improve-log-skeleton` or `html-output`, or spend review time formatting
  cards.
- Every specialist returns a compact, non-HTML review packet to the parent:
  `module`, `verdict` (`clean|findings|blocked|insufficient_evidence`), and
  ordered `findings`. Each finding carries a stable `finding_id`, `target_key`,
  `severity`, plain-language `summary`, exact `evidence`, bounded
  `recommended_fix`, `verification`, and `user_judgment_required` with a reason.
  Include `next_check` for the module. When Pulse invokes the specialist, keep
  the packet within 3000 characters without dropping evidence-backed findings.
- After all selected specialists return, the parent validates evidence,
  deduplicates by `finding_id` and `target_key`, resolves conflicts, applies only
  permitted bounded fixes, and updates `builder/improve.html` once. Preserve one
  attributed card per due module even though the physical HTML patch is
  consolidated.
- A standalone `/review-*` command uses the current Workshop turn as that parent
  coordinator. Its underlying tool or generic reviewer remains read-only; the
  current turn performs the single bounded log update after the review result is
  complete.

### One log: `builder/improve.html`

The workflow keeps a **single durable log** — `builder/improve.html` — the workflow's journal and the user's primary window into it. Everything the user should be able to see later goes here, in one place:

- **applied or proposed changes** (what Pulse Fixer, Goal Advisor, `/improve-*` reviews, and chat fixes did, and why),
- **review findings** (what `/review-*` flagged — recommendations, REVIEW = recommend, do NOT apply),
- **run notes and the recent-run log** (what happened on recent runs),
- **monitor observations** (post-run regressions / drift the monitor caught),
- **Maintenance Radar** (Pulse depth, hygiene watchpoints, and why optional maintenance ran or was skipped),
- **Artifact Review reports** (plan-change artifact drift cursor, clean/no-pending result, or drift findings),
- **answered human decisions** (the question, the user's answer, and the resulting outcome),
- **user rules** (authoritative constraints the user stated),
- **one active Goal Advisor experiment** (the durable 10x/headroom proposal,
  baseline, checkpoint, and measured outcome).

Do not create a separate review document. All review findings, monitor notes, completed decisions, user rules, Artifact Review entries, and run notes belong in `builder/improve.html`. Pending questions are the one exception: store them with `create_human_input_request` in `db/db.sqlite`; Runloop renders them above the HTML.

It is a **self-contained, human-readable HTML document — not Markdown, not a data dump.** This is the page the user opens to understand the workflow, so make it genuinely good to read. Only the parent writer calls `get_reference_doc(kind="html-output")` for the style baseline. When creating or upgrading the file, the parent also loads `get_reference_doc(kind="review-improve-log-skeleton")` and copies that **Starter HTML skeleton** for the exact structure and polish; specialists never load either presentation reference. The Runloop Pulse view renders pending `report_human_inputs` first as **Needs your decision**. The HTML then reads: **two verdicts → status headline → active assumptions challenged (only when any exist) → Today's outcome → collapsed technical details → activity filters → recent runs → newest-first card timeline → collapsed Agent log → archive**. The first screen should read like a daily operator dashboard, not a raw ledger.

The Pulse log is opened in a narrow right panel by default. Design it **mobile-first**:
the base CSS must work at 360-480px with stacked rows, no overlapping metadata, no
desktop-only tables, and long workflow names/ids allowed to wrap. Add desktop/tablet
enhancements with `@media (min-width: ...)`; do not make desktop the default and patch
mobile as an afterthought.

### Four-part Pulse model

Every visible record has one owner in the Japanese-inspired review cycle:

- **Goal — Ikigai:** purpose and success criteria. `soul/soul.md` is the source of truth and Runloop renders it directly. Do not copy a Goal/Profile card into `builder/improve.html`.
- **Signals — Kizuki:** evidence-backed observations from read-only reviewers. State what was found, not what was fixed. Use `data-pulse-section="signals"` and that reviewer's canonical module id.
- **Reflection — Hansei:** what the run means, cadence reasoning, assumptions challenged, and historical question/answer outcomes. Use `data-pulse-section="reflection"`.
- **Improvements — Kaizen:** verified bounded fixes from Pulse Fixer plus Goal Advisor proposals or decisions. Link each improvement to its Signal evidence and verification. Use `data-pulse-section="improvements"` with `pulse_fixer` or `goal_advisor`.

`builder/improve.html` remains one newest-first chronological journal. The Pulse popup groups that same history by these attributes; do not create separate HTML files or duplicate the same narrative across sections.

### The status headline (the 1-second read)

Directly under the verdicts, one `.status` banner carries a **single plain sentence** — the workflow's one-sentence verdict headline (there is no separate verdict file — this banner is the source of truth) — so a user knows "am I OK?" without parsing pills or scrolling. Its `ok|warn|bad` class tracks the **worse** of the two verdicts, and its `.when` shows the run + how long ago. Keep it honest both ways: on a clean, on-target run say so plainly ("Healthy and on-target."); on a regression lead with what's wrong ("Goal drifting — eval 0.78, under 0.90 target for 3 runs."). Never manufacture concern to fill it.

### Freshness — every status says which run it's "as of"

A verdict, a goal-criterion status, or a tile can silently go stale if no recent run measured it. So **stamp the run each status reflects**: the verdict pills carry a small `run #N`, each goal-criterion `.m` line ends with `· run #N`, and the status banner's `.when` shows the run + age. A 4-runs-old "Met" must read as 4-runs-old, not as current truth — this is how the reader tells a live verdict from a stale one.

Different sections may use different evidence dates. The overall status headline reflects the latest run, while a Goal metric may correctly retain the last trustworthy measurement from an older run. In that case show `not measured this run · last measured run #N / YYYY-MM-DD` directly on the card or tile. Every important `.briefitem` and `.tile` needs a visible freshness label; do not rely on nearby sections or buried evidence paths to imply the date. Never replace a known older value with `—` merely because the latest route did not measure it, and never present that older value as current.

### Needs your decision — always first when present

Pending decisions are the most actionable content, so the Runloop Pulse view renders only the currently unanswered `report_human_inputs` above `builder/improve.html`. Use `create_human_input_request`; never build custom form controls inside the static HTML. Title this surface **Needs your decision**. Ask only for a real user/business decision, credential, explicit durable constraint, or material strategy approval — never for a deterministic bug fix, stale path/receipt cleanup, schema wiring, or routine implementation choice.

Do not add a second active-question card to `builder/improve.html`. On the first Pulse after an answer, add a short **Reflection** Decision/Note containing the actual question, the user's selected option and/or free-form answer, and whether the answer is waiting, applied, rejected, or superseded. When consumed, include the concrete outcome from `mark_human_input_consumed`. Historical question/answer detail belongs in Reflection, never in the active-question area.

### Assumptions challenged

Immediately after the status/chips, show an `.assumptions` section only when one or more consequential assumptions are actively limiting the workflow. Each item states: **Assumption**, **Where it came from**, **Evidence for/against**, and **How to validate or retire it**. Distinguish explicit user constraints from agent-inferred choices. Architecture, step shape, providers, channels, thresholds, and tactics are revisable unless the user explicitly approved them as durable constraints.

Do not fill this section with routine implementation facts or generic uncertainty. Keep at most three active assumptions. When resolved, remove the item from the top and record the outcome in the timeline. If no consequential assumption is under challenge, omit the whole visible section; do not render an empty-state card.

### Today's outcome

Below active assumptions, include one `.brief` section with four short cells: **Outcome**, **Goal progress**, **Issues & fixes**, and **Next Pulse**. Each cell is one or two plain sentences and carries freshness where needed. This replaces the vague "What matters now" section and must not duplicate the recent-run row or the latest timeline card. The Maintenance Radar belongs in collapsed technical details, not this summary. Prefer widgets and chips over paragraphs; move raw detail to the timeline or technical section.

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
- `data-kind="run|monitor|maintenance|artifact|decision|advisor|cos|input|open|user|note"` — the primary activity type.

The built-in filter bar searches both recent runs and timeline cards by kind and free text. Dates remain visible and searchable as ordinary text; do not add a separate date picker. Do not remove the static filter script from the skeleton. It is UI behavior, not a legacy JSON data block.

### Two verdicts: Bug and Goal

Every workflow is judged on two independent axes, and the header shows **both** as separate pills — never collapse them into a single "health":

- **Bug** — did it *run correctly*? Errors, skipped steps, missing/empty artifacts, regressions vs the last run. A bug is investigated by **Bug Review** and fixed by the **Pulse Fixer**. Operational, roughly binary.
- **Goal** — is it *achieving its success criteria*? Eval scores and run evidence vs `soul.md`, trending over runs. A goal gap is addressed through **Goal Advisor strategy work**. Continuous.

They are orthogonal: a run can be **Bug: broken** while **Goal: on-target**, or **Bug: clean** while **Goal: short**. Pulse may therefore select Bug Review and Goal Advisor together when operational Bugs and a reliable Goal trend coexist.

Tag each entry with a **Bug** or **Goal** chip when applicable so the fix path is obvious (Bug → Bug Review/Fixer, Goal → Goal Advisor). Also add an action label chip when work was done, proposed, or needs input. Goal Advisor decisions must be visually distinct from Pulse bug-fix notes.

The header verdicts are stable current-state elements, not timeline entries. Update `#pulse-bug-verdict` and `#pulse-goal-verdict` in place on every Pulse; never append another verdict block. If an otherwise current-format `builder/improve.html` is missing either stable verdict element, insert the two-element `.verdicts` block beside the workflow title as a targeted repair. Do not rewrite the whole document or discard its timeline merely to repair missing verdict markup.

### Goal source and progress

`soul/soul.md` is the only durable Goal definition. Runloop's Goal / Ikigai tab renders it directly, so do not duplicate its objective or success criteria as a Goal/Profile card in `builder/improve.html`. The HTML may show time-bound **Goal progress** in Today's outcome, verdicts, Signal evidence, and Reflection entries, each stamped with its evidence run. `/define-success` establishes the goal; Pulse measures and reflects on it without copying the source document.

### Signal tiles — grouped by verdict

Render readable, color-coded signal tiles (value + movement in words: `eval 0.78 -> target 0.90`, `cost 19c -> from 12c`, `wall 4m12s · LLM 2m08s`), grouped into **Bug tiles** (did it run: tests executed, last-run status, runtime), **Goal tiles** (is it achieving: eval scores and output checks vs success criteria), and **Cost/time tiles** (what the run spent: total cost/tokens, wall/LLM/tool time, top-cost step/agent, slowest step/agent). Use `.tile.ok`, `.tile.warn`, `.tile.bad`, `.tile.info`, `.tile.goal`, or `.tile.cost` to make technical detail scannable, and add a visible `.asof` line to every important tile. Read every number from eval reports, run outputs/logs, cost ledgers under `costs/`, and timing summaries under `runs/<run_folder>/logs/<step-id>/execution/` — the deterministic source of truth. Never fabricate a value or a trend, and never use charts.

These tiles, the Maintenance Radar, and detailed evidence live inside one collapsed `<details class="technical">` block labelled **Technical details**. Keep it closed by default so operational internals do not overwhelm the user; do not hide pending decisions, the outcome summary, or important unresolved issues inside it.

### Cost/time readout — one compact operational report per run

Every run row needs the top-level total, and the latest timeline entry or a compact Note should carry the breakdown when there is enough evidence. The goal is a useful CEO/operator read, not a ledger dump.

Use this shape:

- **Run total:** execution + evaluation cost, tokens, wall time, LLM time, tool time, and evidence path.
- **Builder/Pulse overhead:** builder/workshop/Pulse phase cost and tokens from `costs/phase/token_usage.json`, with phase names when available. Keep this visibly separate from run total, and show combined operating cost only when both buckets are labeled.
- **Missing cost evidence:** if execution, evaluation, or builder/Pulse overhead telemetry cannot be
  read, show that bucket as missing and where you looked. Keep known bucket values visible; never
  drop the cost/time section just because one ledger is absent.
- **By plan step:** step id/title, configured tier/model, observed provider/model, cost/tokens,
  wall/LLM/tool time, LLM calls, tool calls.
- **By agent/sub-agent when available:** parent step, agent/sub-agent id/name, model, cost/tokens,
  elapsed time. For `todo_task`, group child agents under the parent plan step.
- **By paid tool when relevant:** provider/model/tool, quantity, estimated/actual cost.

If evidence is missing, say `missing cost evidence`, `missing timing evidence`, or `unpriced provider`; do not estimate. This section is report-only. Do not imply Pulse changed model tiers, prompts, schedules, or agent allocation.

### Agent log — structured handoff at the bottom

The user-facing sections explain outcomes; agents also need compact continuity state. Keep one closed-by-default `<details class="agentlog">` near the bottom, immediately before Archive. Its `#pulse-agent-handoff` body is **current handoff state, not a second report**.

It may contain only:

- current Pulse/run ids and workflow contract version,
- one row per module with last result and next-check condition,
- Artifact Sync Cursor and other durable cursor/anchor ids,
- unresolved finding ids, pending human-input ids, and queued Chief of Staff recommendation ids,
- short evidence paths/links needed by the next agent.

Update this block in place on each Pulse. Never append historical copies. Do not repeat Today's outcome, goal narrative, issue explanations, decisions, or full evidence already visible above; reference their card anchor/path instead. Do not paste raw logs, SQL results, model conversations, report content, or large JSON. Historical narrative stays in the timeline/archive; machine scheduler state stays in SQLite; this bottom block is only the minimal bridge between them.

### Newest on top — always

New entries go at the **top** of the timeline, not appended at the bottom. The file carries a stable anchor comment `<!-- LOG ENTRIES: newest first -->` directly below the header/tiles; insert each new entry immediately after it with `diff_patch_workspace_file`. Never reorder or rewrite existing entries except to close out an open finding (below). **Always read the existing file first** so you continue its style and don't duplicate entries.

Every dated recent-run or timeline record (`.run[data-date]`, `.entry[data-date]`, or `.pulse-record[data-date]`) must include an explicit `data-pulse-section` and `data-module`. Runloop uses these attributes to show the complete history for one review when the user clicks that review in the Pulse popup. Use only the canonical module ids: `run_summary`, `bug_review`, `artifact_review`, `learning_health`, `knowledgebase_health`, `db_health`, `eval_health`, `report_health`, `cost_llm_time`, `llm_ops_review`, `goal_advisor`, or `pulse_fixer`. Use `signals` for reviewer findings, `reflection` for run summaries/questions/measurement, and `improvements` for Goal Advisor or Pulse Fixer decisions. A Pulse batch may update the file once, but it still writes one attributed card per due module; never hide multiple review results inside one mixed card. Do not infer or invent a new module id.

### Entry kinds

Each entry is a small card: a date, a kind tag, optional classification chips, a one-line title, and a short prose body (2–4 sentences, plain language — explain *what* and *why*, link the evidence file or changelog entry when relevant). The first body line must be a `<p class="takeaway">...</p>` that a non-technical operator can understand before reading the details. Use these kinds:

- **Run** — a one-line row in the recent-runs strip: run id, status, key numbers (tests, eval, cost/tokens, wall time), the **backup result** (`backed up ✓ <commit/ref>`, `unchanged — already backed up`, or `backup ✗ <reason>`), and a short note only when something stands out. Routine runs stay terse; flag a run only when it regressed, the backup failed, cost/time evidence is missing, or one step/agent dominates spend/time.
- **Monitor** — a post-run observation: what changed in the output and the most likely cause, correlated against the plan changelog ("output regressed at run N; you tightened step X two runs earlier — likely cause").
- **Maintenance Radar** — a compact Pulse entry explaining how deep this run's post-run stewardship went (`minimal`, `normal`, or `deep`), which hygiene lanes were checked or intentionally skipped, and what concrete evidence should trigger deeper work next time. For every skipped module, show the planned next check in user language (`tomorrow`, `after run X`, or `after N workflow runs`). If Gate overrides an earlier plan, name the new evidence that justified checking early. This is for eval health, learnings, KB, DB/report contracts, report dashboard usefulness, publish/backup/notify setup, model/tier hygiene, and human-input questions. It is not a hidden scheduler; it is an explainable watchlist the next Pulse pass reads before deciding whether to act.
- **Artifact Review** — a report-only Pulse/review entry: changelog range inspected, Artifact Sync Cursor before/after, steps inspected, clean/no-pending result or drift findings, and the recommended next owner. Do not present this entry as a fix that already happened; Artifact Review does not repair artifacts or apply strategy changes itself.
- **Decision** — a change applied or proposed, with the one-line rationale and the file(s) touched. If it fixes an open finding, close that finding out (below). Goal Advisor decisions use `<div class="entry decision">` with tag text `Decision - Goal Advisor - Applied` or `Decision - Goal Advisor - Proposed`; use `<div class="entry decision major">` for material plan changes, report/eval changes that alter user-facing success measurement, cadence/scope changes, or any change the user should notice.
- **Advisor opportunity** — a proposal-only Goal Advisor entry for an out-of-plan idea the current workflow has not considered but an expert operator would raise because it could materially advance the goal. It should be grounded in `soul.md`, run/eval/report evidence, market/process reasoning, or a clearly stated assumption; never present speculation as fact. Record it as `Decision - Goal Advisor - Proposed` with the `Goal` chip and `Advisor idea` work label, and include why it is outside the current plan, what evidence/assumption supports it, the expected upside, and the risk/next decision. Do not auto-apply it from the advisor scan alone.
- **Advisor experiment** — the single durable Goal Advisor 10x/headroom card. Use `class="entry decision major advisor-experiment"`, stable `data-advisor-experiment-id="advisor-exp-<slug>"`, matching `data-input-id="plan-proposal-<slug>"`, `data-status`, and `data-review-after`. Active statuses are `proposed`, `deferred`, `approved`, `running`, `measuring`, and `blocked`; terminal statuses are `adopted`, `rejected`, and `retired`. The visible card contains Current baseline, Current strategy ceiling, 10x thesis, Bounded experiment, Primary success metric, Measurement plan (existing evidence or proposed/applied regular step id + DB contract), Guardrails, Review checkpoint, Rollback condition, Evidence, and Outcome when measured. Update it in place for the full lifecycle. Never leave two active cards and never append a new card for each status transition.
- **Chief of Staff recommendation** — an org-level recommendation written by Chief of Staff / Org Pulse after reading workflow evidence against org goals. Treat it like an external **Open finding**: verify its evidence, then choose the normal builder path (Bug → Pulse Bug Review/Fixer, Goal/strategy → Goal Advisor, measurement gap → eval/report fix, cost/ops → review/apply if safe).
- **Human answer** — after a structured question is answered, add one compact Reflection card containing the actual question, selected option and/or free-form answer, current outcome (`waiting`, `applied`, `rejected`, `superseded`, or `consumed`), and evidence. The unanswered request itself stays only in `report_human_inputs` and Runloop's **Needs your decision** surface. When a later pass uses the answer, call `mark_human_input_consumed` and update the Reflection outcome instead of editing SQLite directly.
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
- `data-suggested-action="bug_review|goal_advisor_proposal|eval_report_measurement_fix|manual_review|no_action_watchpoint|queued_goal_advisor"`
- optional `data-impact`, `data-effort`, and `data-status-*` attributes written by the marker tool

Lifecycle statuses:

- `proposed` — Org Pulse suggested it; workflow Pulse has not reviewed it yet.
- `accepted` — workflow Pulse agrees the recommendation is valid but has not routed it yet.
- `queued_goal_advisor` — strategic/Goal work that the Pulse-selected Goal Advisor module should consider.
- `in_progress` — a builder/goal-advisor action is currently addressing it.
- `needs_evidence` — the recommendation may be right but lacks enough evidence or measurement.
- `done` — workflow Pulse or Goal Advisor handled it and cited the confirming evidence.
- `dismissed` — workflow Pulse rejected it with a reason.
- `blocked` — valid, but waiting on user input, credentials, external data, or another dependency.

Workflow Pulse and Goal Advisor must use `mark_cos_recommendation_status` to change these statuses instead of hand-editing lifecycle attributes. They may still add a visible Decision/Monitor entry that explains the action, but the status marker is the machine-readable reply that Org Pulse reads next time.

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
  data-suggested-action="goal_advisor_proposal">
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

- `worklabel bugfix` → `Bug fix`: Pulse Fixer changed prompts/config/guards/validation/code shape to make the workflow run correctly.
- `worklabel improvement` → `Improvement`: Goal Advisor or a builder change improved strategy, plan quality, success criteria alignment, cadence, or user-facing usefulness.
- `worklabel advisor` → `Advisor idea`: Goal Advisor proposed an out-of-plan expert recommendation or unconventional opportunity that could help the goal but needs user choice or stronger evidence before changing the plan.
- `worklabel artifact` → `Artifact drift`: Artifact Review found or resolved plan-change drift in reports, evals, KB/learnings, saved code, db wiring, or generated HTML.
- `worklabel report` → `Report fix`: report dashboard/query/HTML/data-binding repair.
- `worklabel eval` → `Eval fix`: evaluation rubric, eval wiring, route scoping, or score evidence repair.
- `worklabel cost` → `Cost/time`: LLM/model/cost/time telemetry observation or repair.
- `worklabel maintenance` → `Maintenance`: Pulse hygiene/radar decision, especially when it intentionally skipped optional work on high-frequency schedules or escalated to a deeper check.
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

- `<div class="entry decision">` for normal applied/proposed Goal Advisor decisions.
- `<div class="entry decision major">` when the decision changes plan strategy, report/eval measurement, workflow cadence/scope, user-facing dashboard interpretation, or materially affects cost/quality/risk.
- `.tag.decision` with one of these exact labels: `Decision - Goal Advisor - Applied`, `Decision - Goal Advisor - Proposed`, `Decision - Pulse fix`, or `Decision - Manual`.
- A verdict chip plus an action label chip. Examples: a Pulse fix uses `<span class="kind bug">Bug</span><span class="worklabel bugfix">Bug fix</span>`; a Goal Advisor plan change uses `<span class="kind goal">Goal</span><span class="worklabel improvement">Improvement</span>`.
- A `<p class="takeaway">...</p>` before the grid that says the decision in one user-readable sentence.
- `.decisiongrid` rows for the fixed fields: **Why now**, **Evidence**, **Change**, **Expected impact**, **Files touched**, and **Risk / gap**. Omit a row only when it truly does not apply; do not bury these fields in prose. Each field should be one short sentence; if a field needs raw technical detail, put the human meaning first and the raw path/id second.

Example:

```html
<div class="entry decision major">
  <div class="ehead">
    <span class="tag decision">Decision - Goal Advisor - Applied</span>
    <span class="kind goal">Goal</span>
    <span class="worklabel improvement">Improvement</span>
    <span class="etitle">Replanned lead-scoring around verified replies</span>
    <span class="when">2026-07-02 · Goal Advisor</span>
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

A Decision card records what a Pulse fix or Goal Advisor plan change applied and *why* — but a journal that only ever says "applied X" never proves the system is working. So a Decision that changed behaviour stays **unconfirmed** until a later run measures its effect, and then it gets **one** outcome stamp added in place (never a second one, never a new card):

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

The log must not grow without bound. Before Gate, the scheduler conditionally sends a dedicated archive turn when `builder/improve.html` has **more than 20 timeline entries** and at least one older resolved entry is safe to move. Byte size and line count do not trigger archiving. That turn decides semantically what can move; normal Gate/module turns should not improvise a second archive pass.

`builder/improve.html` remains the authoritative **current** Pulse view. Keep its complete top dashboard, current metrics/freshness, all open findings, user rules, current notes, unresolved or unconfirmed decisions, unanswered or not-yet-consumed human questions, the newest **20** timeline cards, and at least the newest **5** recent-run rows. Move only older **resolved** findings, superseded confirmed decisions, and routine old run rows into self-contained monthly archives at `builder/improve-archive/YYYY-MM.html`.

Archive safely:

1. Read the active file and any existing target-month archive in full.
2. Stage complete active and archive HTML documents in temporary files under `builder/`; archives are never bare card fragments.
3. Verify every moved card/run appears exactly once across active plus archive, both documents are non-empty and contain `html`, `head`, and `body`, and no protected/open item moved.
4. Only after validation, replace the monthly archive and then the active file. Never truncate the original first.
5. Add or update one compact Archive Index link using `href="improve-archive/YYYY-MM.html"`, with date range and moved-item count. Merge into an existing month without duplicates and keep entries newest first.

If the file crossed a mechanical threshold but has no safely archivable history, leave it unchanged and report that plainly. The active file must always answer "what is the workflow's state now, and what still needs attention?"

### Upgrading an old-format log (one-time, REQUIRED before appending)

An existing `builder/improve.html` is **old-format** — and must be upgraded, not appended to — if it has **any** of:

- a title like "Improvement Ledger";
- `## Active Improvement Index` / `## Recent Entries` / `## Archive Index` headings;
- ```improve-decision``` fenced/`<script>` JSON blocks;
- `F-…` / `I-…` ids;
- legacy Markdown improve logs;
- its own ad-hoc CSS (`.summary` / `.badge` / `.stats`, system-ui body) instead of the skeleton's;
- no `<meta name="viewport">`;
- missing `data-pulse-schema="2"` on the root `<html>` element;
- missing mobile-first stacked `.status` / `.run` / `.entry` layouts or prose-safe overflow rules;
- an `.etitle` rule missing `flex:1 1 auto`, or an `.ehead > .when` rule that keeps `margin-left:auto` / `white-space:nowrap` in the base mobile CSS. That older skeleton collapses entry titles and body text into narrow columns beside timestamp metadata, leaving the card half-empty in the right panel.
- any recent-runs table/flex/grid whose date/status/type/age metadata can shrink into one-character columns. This usually comes from global `overflow-wrap:anywhere` on `body`, `td`, or metadata cells. Rewrite those rows as stacked/mobile-first cards or keep metadata/chips non-wrapping (`white-space:nowrap; overflow-wrap:normal; word-break:normal`) while only prose/evidence fields use `overflow-wrap:anywhere`.
- any recent-runs desktop layout that puts the long `.note`/evidence text beside date/status/type/age metadata. The note must sit on a full-width second row so the run list stays readable in both the right panel and a wide browser.
- missing `.filters` UI or missing `data-date`, `data-kind`, `data-pulse-section`, or `data-module` attributes on recent-run rows and timeline entries. Add the Kind/Search/Reset filter bar (no date picker) and backfill dates/kinds/modules/sections from visible dates, run folders, entry labels, or best available evidence. Do not silently default every unclassified historical card to Bug Review; preserve it as `run_summary`/`reflection` when no specific reviewer can be established.
- missing `.worklabel` CSS/action-label examples. Current logs need action chips such as `Bug fix`, `Improvement`, `Advisor idea`, `Artifact drift`, `Report fix`, `Eval fix`, `Cost/time`, `Backup/publish`, `Needs input`, and `Manual` so the user can scan what kind of work happened.
- a text-heavy first screen, a visible `What matters now` heading instead of `Today's outcome`, signal/cost/Maintenance tiles outside a closed-by-default `.technical` details block, no optional `.assumptions` support, no bottom `#pulse-agent-handoff`, or recent runs rendered as a dense table. Upgrade it to the current human-first dashboard shell before appending new entries.

Missing `#pulse-bug-verdict` or `#pulse-goal-verdict` alone does **not** require a full old-format rewrite when the rest of the current skeleton is intact. Insert the standard `.verdicts` block in place and preserve all existing cards, filters, and history.

**Do NOT append your new entry into the old structure** — that produces good content in a stale, off-brand shell. Instead, **rewrite the entire document using `get_reference_doc(kind="review-improve-log-skeleton")`** as a one-time upgrade:

1. Read the old file in full.
2. Load `get_reference_doc(kind="review-improve-log-skeleton")` and write the skeleton fresh: header + two verdict pills, status headline, optional active Assumptions challenged section, the Today's outcome brief, collapsed Technical details, filter bar, the recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, the collapsed Agent log, and the archive section. Goal remains in `soul/soul.md` and is rendered by Runloop's Goal / Ikigai tab.
3. Carry every **unresolved finding** and **still-relevant recent decision/run** forward as timeline **cards** (newest first), dropping the `<script>` JSON blocks and the `F-`/`I-` ids — write readable prose, give an open finding a short anchor id only.
4. Delete any legacy `.md` (`execute_shell_command`) so nothing is duplicated.

After this one rewrite the file is in skeleton format; from then on you just prepend cards. The structured JSON schema and the dual `F-/I-` id system are retired.

### Starter HTML skeleton

The full copy-paste HTML skeleton lives in `get_reference_doc(kind="review-improve-log-skeleton")`. Load it when creating `builder/improve.html` or doing the required one-time old-format upgrade. Keep this reference doc focused on log semantics; the skeleton reference keeps the CSS, filter script, and card examples in one place without bloating every normal review/improve prompt.
