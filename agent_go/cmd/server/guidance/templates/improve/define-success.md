Define what success means for this workflow before optimization.

Write to `builder/improve.html` — the single durable log. For the log/HTML format, the one-time migration (folding any legacy `builder/review.html` findings in), and how entries are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

Either bootstrap the auto-improvement framework (one-time configuration: Workflow Profile + metrics) or, if the framework is already in place, audit the existing setup and surface issues.{{if .Focus}}

Focus / hints from user: {{.Focus}}{{end}}

DISCOVERY (read-only)
1. Read workflow.json. Note any existing oversight_mode.
2. Read builder/improve.html if present — note any existing Workflow Profile block, recent timeline entries, open findings, and archive rows. If it's short, read it in full.
3. Read soul/soul.md to extract the workflow's objective and success_criteria.
4. Read planning/plan.json — note the steps, their types, and overall structure (frozen plan vs in flux vs explore/exploit).
5. Read evaluation/evaluation_plan.json if present — eval steps will be the natural source for many starter metrics.
6. Read <workflow>/planning/metrics.json if present.
7. Read <workflow>/db/metrics_history.jsonl if present (the per-run snapshot history written automatically after each successful eval).
8. Read runs/ to see how mature the workflow is.

STEP 0 — DETECT SETUP STATE AND BRANCH
After Discovery, decide which mode this command runs in:

- **FRESH SETUP** — `builder/improve.html` has no Workflow Profile block (no declared primary type) AND `planning/metrics.json` is absent or empty. Proceed to STEP 1.
- **REVIEW EXISTING** — `builder/improve.html` already has a Workflow Profile block AND/OR `planning/metrics.json` already has metrics declared. **Skip STEPS 1–4 and go to STEP 5 (REVIEW PATH)** — do not re-bootstrap a workflow that already has a framework configured. Audit instead.
- **PARTIAL** — one is present, the other isn't. Run STEP 5 first to surface what's there, then walk the user through completing the missing piece (Profile if absent → STEP 2; metrics if absent → STEP 4).

STEP 0.5 — Confirm the goal with the user (the gate everything else rests on)
The auto-improve loop optimizes toward THIS goal, so a vague or stale one makes the whole loop aimless. Establish a real, user-confirmed goal before classifying or scheduling anything — do not infer it silently from `soul.md` and move on.

- Show the user the workflow's **objective** and each **success_criterion** read from `soul.md`, and ask: "Is this still what success means here? Anything to change, add, or drop?"
- Push for **measurable** criteria — a metric, threshold, or checkable outcome, not "works well." Refine vague ones together.
- **If `soul.md` has no success_criteria, or they're vague/placeholder, STOP and ask directly: "What does success look like for this workflow — what measurable outcomes tell you it's working?"** Do not proceed to classification or scheduling without an answer.
- Write the confirmed objective + success_criteria back to `soul.md` — it's the single source of truth that the goal card and the Goal verdict both read from.

Only once the goal is confirmed, continue to STEP 1.

STEP 1 — Classify the workflow profile
Walk the user through a **primary type** plus optional **secondary traits**, then map that to the internal axes. Real workflows mix types; do not force a single enum.

Ask the user to confirm:

- **Primary type** — the main improvement strategy:
  - `deterministic_harden_first`: known plan/output; improve reliability, validation, and locking.
  - `open_metric_optimization`: goal known but best plan unknown; improve by experiments, outcome metrics, and replanning.
  - `business_context_accumulating`: workflow improves by remembering user rules, preferences, examples, account/domain context.
  - `compliance_audit`: correctness, evidence, traceability, and conservative change control matter most.
  - `human_review_production`: workflow prepares drafts/options for human approval; improve approval rate and reduce edit burden.
  - `monitoring_alerting`: workflow watches events/thresholds and escalates; improve false positives/negatives and alert latency.
  - `research_synthesis`: workflow gathers uncertain external info and produces grounded judgment; improve source quality and unsupported-claim checks.
  - `creative_generative`: subjective output quality and preference fit matter most; improve via feedback and examples.
- **Secondary traits** — any additional types that materially constrain improvement. Usually 0–3.

Then map the confirmed type/traits onto the internal axes:

- **Plan stability** — `mutable` (plan changes freely), `ratchet` (additions only — compliance, security), `frozen` (no plan-shape change without explicit user OK).
- **Runtime mode** — `single` (one plan, runs as-is) vs `dual` (alternates explore / exploit; e.g. social-media trying new tactics weekly then exploiting the winner).
- **Business context accumulation** — `accumulating` when the workflow should persist user-supplied rules/preferences/examples/context; otherwise `none`.
- **Improvement cadence** — how often this workflow is expected to improve: daily / weekly / per-incident / quarterly / never (frozen).

Show your inference + reasoning + the alternative answers you considered for primary type, secondary traits, and each axis. Ask the user to confirm.

STEP 2 — Seed builder/improve.html (the single workflow log)
If `builder/improve.html` does not exist yet, create it from the **Starter HTML skeleton** in `get_reference_doc(kind="review-improve-log")` — write that document verbatim with `diff_patch_workspace_file` (do NOT `mkdir` via shell; use workflow-relative paths). If the file already exists, edit the goal card / profile in place — don't overwrite the timeline.

Fill, in the skeleton:
- **Header** — workflow name, the type/oversight chips, and both **verdict pills**. With no runs yet, set Bug = "Bug-free" and Goal = "Not yet measured" (warn) — be honest that the goal is unproven until the first run produces eval/metric evidence.
- **The goal card** — the one-line **objective** from `soul.md` in `.obj`, then one `.crit` row per **success criterion** from `soul.md`. Until the first run, mark each criterion status as `short` ("not yet measured — needs a run") rather than `met`; the metric/evidence note can name the metric that will measure it.
- **The Workflow Profile** — append a short readable profile block right after the goal card (a small labelled section or `<div class="entry">`) with:

- **Primary type** — <chosen> — <one-line rationale>
- **Secondary traits** — <list or "none"> — <one-line rationale>
- **Plan stability** — <mutable | ratchet | frozen> — <one-line rationale>
- **Runtime mode** — <single | dual; if dual, name the modes> — <one-line rationale>
- **Business context** — <accumulating | none> — <one-line rationale>
- **Improvement cadence** — <chosen> — <one-line rationale>
- **Behavioural implications the agent respects every turn** — 3–5 short lines, e.g. "Use harden_workflow for local reliability/contract failures; replan only when outcome trends show the path itself is weak"; "Plan is ratchet — do not delete_plan_steps without explicit user approval"; "Recognise user-supplied rules in conversation and offer capture_context."

Leave the signal tiles, recent-runs strip, the `<!-- LOG ENTRIES: newest first -->` anchor, and the archive section in place and empty — they fill in after the first run and as work accrues. Do not invent metric values or runs that haven't happened.

STEP 3 — Set the framework fields in workflow.json
These are the only structured framework fields; they drive real behavior.

- `oversight_mode` — `manual` / `supervised` (default) / `autonomous`. Recommended defaults: deterministic + ratcheting workflow → `manual`; exploratory → `autonomous`; contextual / business-context → `supervised`.
- `post_run_monitor` — `true` / `false`. **Opt-in** (omit/false = off). Set `true` for workflows where a silently-broken or drifting run would matter and isn't watched live: scheduled QA, production, monitoring/alerting, compliance, and any business-critical workflow on a cron. Leave off for scratch, experimental, or interactive-only workflows where the extra per-run triage pass isn't worth it. When on, after each scheduled run a cheap read-only monitor records Bug + Goal verdicts and any finding into `builder/improve.html`. Recommend a value based on the profile, but it's the **user's choice** — confirm it, and tell them they can flip it anytime.

When you turn `post_run_monitor` on, also **ask the user how they want to be notified** — by default the monitor pings them once only on a transition (broke / recovered / new finding) and is silent on steady runs. If they want something different ("every run with the eval score", "only when it breaks, never on recovery", "always include the Pulse log link", "don't notify at all"), capture that in plain language as a `## Notifications` section in `soul/soul.md` (`diff_patch_workspace_file`). The monitor reads it and obeys, overriding the default. If they're happy with the default, leave the section out.

STEP 4 — Bootstrap metrics.json
Behavior depends on the profile from Step 1:

- Primary `deterministic_harden_first` or plan stability `ratchet`/`frozen` + business context `none`: propose 3–5 SLO-mode metrics from the workflow's own outcomes — success-rate (floor), schema/file validity, data freshness. Source: `eval_step`. (Do not add cost/duration here — see the cost/duration note below.)
- Primary `open_metric_optimization`: propose 3–5 outcome metrics derived from success_criteria plus 1–2 operational SLOs. Outcome metrics should be target-mode where the workflow is trying to move a number; they drive experiments and replans.
- Primary `business_context_accumulating` or business context `accumulating`: REQUIRED. Propose 3–5 outcome + rule-conformance metrics derived from success_criteria. Mix outcome metrics (mode=`target`, drive toward a value) with SLO metrics (mode=`slo`, stay above floor / below ceiling) — outcome metrics drive progress, SLOs enforce constraints.
- Primary `compliance_audit`: propose evidence-completeness, false-negative, traceability, and policy-coverage SLOs. Prefer supervised/manual oversight, and keep the improve-ledger forensic (never edit past entries, only append).
- Primary `human_review_production`: propose approval-rate, revision-count/edit-burden, provenance completeness, and draft-quality metrics.
- Primary `monitoring_alerting`: propose false-positive, false-negative/missed-alert, alert-latency, and escalation-quality metrics.
- Primary `research_synthesis`: propose citation/source freshness, source diversity, unsupported-claim count, and synthesis-usefulness metrics.
- Primary `creative_generative`: propose human rating, style adherence, preference-match, and variant-performance metrics; keep thresholds softer unless the user has explicit quality bars.
- **Cost/duration metrics — do NOT auto-add.** Per user preference, `cost_per_run` / `run_duration_seconds` (and other operational telemetry) are not seeded by default. Only add them if the user explicitly asks for cost/time tracking — confirm the source with them then (telemetry vs an eval-step wrapper), and set the threshold from real data (see the threshold gotcha below), never a guessed number.

Metric roles:
- Mark only 1–4 metrics as `role="primary"`: the north-star outcome metrics and any must-not-break guardrail whose failure invalidates the workflow. These are what improvement loops optimize first.
- Mark diagnostic, explanatory, operational, and coverage metrics as `role="secondary"`. Secondary metrics explain why primary metrics moved or prevent regressions, but they should not crowd out the primary objective.
- Add `category` so the UI and future agents can group the signal. Use concise values such as `outcome`, `execution`, `guardrail`, `content_quality`, `strategy_learning`, `telemetry`, or a workflow-specific equivalent.

For each proposed metric, supply id + label + role + category + unit + direction + mode + threshold + source + `success_criteria` (quote or summarize the soul.md success criterion it measures). Use `propose_metric` to write each one — never shell-write `planning/metrics.json` (it's folder-guarded). Common gotchas to avoid:
- **Set thresholds from real data, not guesses.** Before choosing a floor/ceiling/target, read the actual recent values — `db/metrics_history.jsonl` for this metric (or the latest `scores/evaluation/` report, or the run's telemetry) — and set the number from those actuals plus a sensible buffer (e.g. cost/duration ceiling ≈ recent median × 1.5; success-rate floor just under the recent achieved rate). If there are **no runs yet** (fresh bootstrap), do not invent a number — propose the metric and note the threshold as "to set after first run", then set it on the next pass once `db/metrics_history.jsonl` has data. A made-up threshold produces false breaches or a meaningless bar.
- For `source.type=eval_step`, prefer explicit structured-output keys emitted by the eval step's JSON output. Legacy final-score fields are not produced by new eval runs.
- Telemetry source: only six wired fields exist (`run.total_cost_usd`, `run.duration_seconds`, `eval.total_cost_usd`, `eval.duration_seconds`, `total.cost_usd`, `total.duration_seconds`). Other names silently return no value.

STEP 5 — REVIEW PATH (when framework is already set up)
You're auditing existing setup, not bootstrapping. Walk through these checks and surface any issues with proposed fixes. Apply nothing without user confirmation.

5.1 — **Workflow Profile sanity**
- Is the existing Workflow Profile block still accurate given the current plan? If the workflow has evolved (steps added/removed, mode changed) but the profile is stale, propose updating it.
- Are primary type, secondary traits, and the four axes filled in with rationale, or are some empty / placeholder?
- Are the behavioral implications still relevant?

5.2 — **Framework fields**
- Verify `oversight_mode` in workflow.json matches the workflow profile. A "ratchet" stability with `oversight_mode: autonomous` is mismatched and should be flagged.
- Check `post_run_monitor`. If the workflow is scheduled and a silent break would matter (QA, production, monitoring, compliance) but it's off, recommend turning it on. If it's a scratch/experimental workflow with the monitor on and the user is watching every run anyway, note they can turn it off to save the per-run pass. It's the user's call either way.

5.3 — **Metric definitions**
- Read every entry in `planning/metrics.json::metrics[]`.
- For each metric, validate: id is unique kebab.dot, unit is sensible, direction matches (e.g. `cost_per_run` should be `lower_better`), mode + threshold is consistent (target requires `target`, slo+higher_better requires `floor`, slo+lower_better requires `ceiling`).
- For each metric, verify `role` is present and one of `primary` / `secondary`. If there are more than 4 primary metrics, recommend demoting diagnostic metrics to secondary. If there are zero primary metrics, recommend promoting the actual north-star outcome metric.
- For each metric, verify `category` is present and useful for grouping (outcome/execution/guardrail/content_quality/strategy_learning/telemetry/etc.).
- For each metric, verify `success_criteria` is present and clearly links to a `soul.md` success criterion. Missing linkage is a framework issue: the UI will warn because the metric is not anchored to a user outcome.
- Does the source point at something real? `eval_step` source must reference an existing eval step id; `telemetry` source must use one of the six wired fields.
- Are there obvious gaps — success criteria from soul.md that no metric measures? Surface as coverage suggestions (don't auto-add).

5.4 — **Metric health (resolve errors)** — most important pass
Read the most recent rows of `db/metrics_history.jsonl` for each metric id. For each row with `has_value: false` and a `resolve_error`, categorize:

- "no structured output (field=X)" — the metric specifies `field=X` but the eval step does not emit that key in structured output. Two fix paths:
  (a) Update the eval step's Python to emit a structured JSON containing `X` (preferred — measures the named outcome explicitly). This is an eval-side change; recommend running `/improve-evaluation` with focus on that step.
  (b) Retire the metric if it no longer represents a real outcome.
- "eval step <id> not found" — id mismatch. Either restore the eval step or retire the metric and propose a new one with the correct id.
- Consistent NO VALUE without resolve_error — the eval step didn't run or produced no metric-ready value. Operational coverage gap; flag for `/improve-evaluation`.

For each broken metric, name the metric, the resolve_error, and the recommended fix. Apply only after user confirms.

5.5 — **Unused / orphan metrics**
- Are any metrics in `planning/metrics.json` stale, duplicated, or not represented in recent `db/metrics_history.jsonl` rows? If they're outcome metrics (i.e., genuinely meant to be tracked), that's fine — but flag metrics with no recent values, repeated resolve errors, or unclear success-criteria linkage.

5.6 — **Threshold sanity (data-driven)**
- For each SLO/target metric, compare its threshold to the actual recent values in `db/metrics_history.jsonl`. Flag thresholds that are obviously made-up — never breached, always breached, or set before any run existed — and recommend resetting them from real data (recent median/achieved rate ± a buffer).
- Do **not** suggest adding `cost_per_run` / `run_duration_seconds` — operational cost/duration metrics are not auto-added (user preference). Only add them if the user explicitly asks, and then prefer an eval-step wrapper.

After STEP 5 — Record what you reviewed and recommended in `builder/improve.html` as a prose timeline entry (a dated "Framework review" entry) summarizing the review, so the audit trail survives the session.

If existing `builder/improve.html` is already long, preserve it as the log but compact it after the review:
- keep the Workflow Profile, the latest ~10–20 timeline entries, and all open findings in `builder/improve.html`
- move older resolved/no-action/superseded entries to `builder/improve-archive/YYYY-MM.html`
- preserve prior entries in the archive
- leave an archive row with date range, entry count, any still-unresolved findings, and a one-line summary
- keep open findings, current metric/eval gaps, and the latest semantic plan/eval/metric changes in the root file
