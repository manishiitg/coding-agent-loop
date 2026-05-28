Define what success means for this workflow before optimization.

Before writing builder/improve.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown.

MIGRATION (one-time): Check whether builder/improve.md exists. If it does, read it, extract all unresolved entries and structured blocks, incorporate them into builder/improve.html, then delete builder/improve.md with execute_shell_command. Also check for builder/review.md and migrate it to builder/review.html the same way.

Either bootstrap the auto-improvement framework (one-time configuration: Workflow Profile + metrics) or, if the framework is already in place, audit the existing setup and surface issues.{{if .Focus}}

Focus / hints from user: {{.Focus}}{{end}}

DISCOVERY (read-only)
1. Read workflow.json. Note any existing oversight_mode / decision_log_mutability.
2. Read builder/improve.html if present — note any existing "## Workflow Profile" section, Active Improvement Index, Archive Index, and recent entries. If there is no index/retention structure yet, read the file in full.
3. Read soul/soul.md to extract the workflow's objective and success_criteria.
4. Read planning/plan.json — note the steps, their types, and overall structure (frozen plan vs in flux vs explore/exploit).
5. Read evaluation/evaluation_plan.json if present — eval steps will be the natural source for many starter metrics.
6. Read <workflow>/planning/metrics.json if present.
7. Read <workflow>/db/metrics_history.jsonl if present (the per-run snapshot history written automatically after each successful eval).
8. Read runs/ to see how mature the workflow is.

STEP 0 — DETECT SETUP STATE AND BRANCH
After Discovery, decide which mode this command runs in:

- **FRESH SETUP** — `builder/improve.html` has no "## Workflow Profile" section AND `planning/metrics.json` is absent or empty. Proceed to STEP 1.
- **REVIEW EXISTING** — `builder/improve.html` already has a "## Workflow Profile" section AND/OR `planning/metrics.json` already has metrics declared. **Skip STEPS 1–4 and go to STEP 5 (REVIEW PATH)** — do not re-bootstrap a workflow that already has a framework configured. Audit instead.
- **PARTIAL** — one is present, the other isn't. Run STEP 5 first to surface what's there, then walk the user through completing the missing piece (Profile if absent → STEP 2; metrics if absent → STEP 4).

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

STEP 2 — Write the Workflow Profile to builder/improve.html
Append (or replace, if a section already exists) the following section in builder/improve.html. Use `diff_patch_workspace_file` — do NOT `mkdir` via shell. Use workflow-relative paths.

```markdown
## Workflow Profile (auto-improvement framework)

- **Primary type**: <chosen> — <one-line rationale>
- **Secondary traits**: <comma-separated list or "none"> — <one-line rationale>
- **Plan stability**: <chosen> — <one-line rationale>
- **Runtime mode**: <single | dual> — <one-line rationale; if dual, name the modes: e.g., "explore (weekly reset) / exploit (daily default)">
- **Business context**: <accumulating | none> — <one-line rationale>
- **Improvement cadence**: <chosen> — <one-line rationale>

Behavioral implications the agent should respect on every turn:
- <primary-type implication, e.g. "Use harden_workflow for local reliability/contract failures; use replan_workflow_from_results when outcome trends show the workflow path or strategy is weak.">
- <secondary-trait implication, e.g. "Because this is also human-review production, track approval/edit burden and preserve provenance.">
- <plan-stability implication, e.g. "Do not call replan_workflow_from_results or delete_plan_steps without explicit user approval.">
- <runtime-mode implication, e.g. "When dual: branch step behavior on the workflow's chosen runtime signal.">
- <business-context implication, e.g. "Recognize user-supplied rules in conversation and offer capture_context.">

## Active Improvement Index

- **Current focus:** setup pending
- **Open findings / hypotheses:** none yet
- **Current metric/eval gaps:** to be filled after first eval/metric review
- **Latest semantic change:** none
- **Recent evidence window:** iteration-0 after the next run

## Archive Index

| Archive | Date range | Entries | Unresolved ids | Summary |
| --- | --- | ---: | --- | --- |

## Recent Entries
```

STEP 3 — Set the two hard-gate fields in workflow.json
These are the only structured framework fields; they drive real behavior.

- `oversight_mode` — `manual` / `supervised` (default) / `autonomous`. Recommended defaults: deterministic + ratcheting workflow → `manual`; exploratory → `autonomous`; contextual / business-context → `supervised`.
- `decision_log_mutability` — `append_only` (default) / `append_only_strict`. Set strict ONLY for compliance / audit workflows where structured improve.html decision entries are forensic.

STEP 4 — Bootstrap metrics.json
Behavior depends on the profile from Step 1:

- Primary `deterministic_harden_first` or plan stability `ratchet`/`frozen` + business context `none`: propose 3–5 SLO-mode metrics — success-rate (floor), schema/file validity, data freshness, `cost_per_run` (ceiling), `run_duration_seconds` (ceiling). Source: `telemetry` for cost/duration, `eval_step` for the rest.
- Primary `open_metric_optimization`: propose 3–5 outcome metrics derived from success_criteria plus 1–2 operational SLOs. Outcome metrics should be target-mode where the workflow is trying to move a number; they drive experiments and replans.
- Primary `business_context_accumulating` or business context `accumulating`: REQUIRED. Propose 3–5 outcome + rule-conformance metrics derived from success_criteria. Mix outcome metrics (mode=`target`, drive toward a value) with SLO metrics (mode=`slo`, stay above floor / below ceiling) — outcome metrics drive progress, SLOs enforce constraints.
- Primary `compliance_audit`: propose evidence-completeness, false-negative, traceability, and policy-coverage SLOs. Prefer strict improve-ledger mutability and supervised/manual oversight.
- Primary `human_review_production`: propose approval-rate, revision-count/edit-burden, provenance completeness, and draft-quality metrics.
- Primary `monitoring_alerting`: propose false-positive, false-negative/missed-alert, alert-latency, and escalation-quality metrics.
- Primary `research_synthesis`: propose citation/source freshness, source diversity, unsupported-claim count, and synthesis-usefulness metrics.
- Primary `creative_generative`: propose human rating, style adherence, preference-match, and variant-performance metrics; keep thresholds softer unless the user has explicit quality bars.
- Always include `cost_per_run` and `run_duration_seconds` as telemetry SLOs when the telemetry source is supported for this workflow surface; if telemetry metrics cannot resolve yet, surface that as a framework gap rather than creating noisy broken metrics.

Metric roles:
- Mark only 1–4 metrics as `role="primary"`: the north-star outcome metrics and any must-not-break guardrail whose failure invalidates the workflow. These are what improvement loops optimize first.
- Mark diagnostic, explanatory, operational, and coverage metrics as `role="secondary"`. Secondary metrics explain why primary metrics moved or prevent regressions, but they should not crowd out the primary objective.
- Add `category` so the UI and future agents can group the signal. Use concise values such as `outcome`, `execution`, `guardrail`, `content_quality`, `strategy_learning`, `telemetry`, or a workflow-specific equivalent.

For each proposed metric, supply id + label + role + category + unit + direction + mode + threshold + source + `success_criteria` (quote or summarize the soul.md success criterion it measures). Use `propose_metric` to write each one — never shell-write `planning/metrics.json` (it's folder-guarded). Common gotchas to avoid:
- For `source.type=eval_step`, prefer explicit structured-output keys emitted by the eval step's JSON output. Legacy final-score fields are not produced by new eval runs.
- Telemetry source: only six wired fields exist (`run.total_cost_usd`, `run.duration_seconds`, `eval.total_cost_usd`, `eval.duration_seconds`, `total.cost_usd`, `total.duration_seconds`). Other names silently return no value.

STEP 5 — REVIEW PATH (when framework is already set up)
You're auditing existing setup, not bootstrapping. Walk through these checks and surface any issues with proposed fixes. Apply nothing without user confirmation.

5.1 — **Workflow Profile sanity**
- Is the existing "## Workflow Profile" still accurate given the current plan? If the workflow has evolved (steps added/removed, mode changed) but the profile section is stale, propose updating it.
- Are primary type, secondary traits, and the four axes filled in with rationale, or are some empty / placeholder?
- Are the behavioral implications still relevant?

5.2 — **Hard-gate fields**
- Verify `oversight_mode` and `decision_log_mutability` in workflow.json match the workflow profile. A "ratchet" stability with `oversight_mode: autonomous` is mismatched and should be flagged.

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

5.6 — **Telemetry SLOs present?**
- If `cost_per_run` and `run_duration_seconds` (or `total.cost_usd` / `total.duration_seconds`) aren't defined as telemetry SLOs, suggest adding them. Free signal.

After STEP 5 — Record what you reviewed and recommended in `builder/improve.html` under a "## Framework review YYYY-MM-DD" subsection so the audit trail survives the session. Include a structured `improve-decision` fenced JSON block summarizing the review.

If existing `builder/improve.html` is already long, preserve it as the ledger but compact it after the review:
- keep Workflow Profile, Active Improvement Index, Archive Index, and latest 10-20 detailed entries in `builder/improve.html`
- move older resolved/no-action/repeated detailed entries to `builder/improve-archive/YYYY-MM.html`
- preserve structured `improve-decision` blocks in the archive
- leave Archive Index rows with date range, entry count, unresolved ids, and summary
- keep unresolved findings, active hypotheses, current metric/eval gaps, and latest semantic plan/eval/metric changes in the root file
