Set up — or review — the auto-improvement framework on this workflow. Either bootstrap the framework (one-time configuration) or, if the framework is already in place, audit the existing setup and surface issues.{{if .Focus}}

Focus / hints from user: {{.Focus}}{{end}}

DISCOVERY (read-only)
1. Read workflow.json. Note any existing oversight_mode / decision_log_mutability.
2. Read builder/improve.md if present — note any existing "## Workflow Profile" section.
3. Read soul/soul.md to extract the workflow's objective and success_criteria.
4. Read planning/plan.json — note the steps, their types, and overall structure (frozen plan vs in flux vs explore/exploit).
5. Read evaluation/evaluation_plan.json if present — eval steps will be the natural source for many starter metrics.
6. Read <workflow>/planning/metrics.json if present.
7. Read <workflow>/db/metrics_history.jsonl if present (the per-run snapshot history written automatically after each successful eval).
8. Read runs/ to see how mature the workflow is.

STEP 0 — DETECT SETUP STATE AND BRANCH
After Discovery, decide which mode this command runs in:

- **FRESH SETUP** — `builder/improve.md` has no "## Workflow Profile" section AND `planning/metrics.json` is absent or empty. Proceed to STEP 1.
- **REVIEW EXISTING** — `builder/improve.md` already has a "## Workflow Profile" section AND/OR `planning/metrics.json` already has metrics declared. **Skip STEPS 1–4 and go to STEP 5 (REVIEW PATH)** — do not re-bootstrap a workflow that already has a framework configured. Audit instead.
- **PARTIAL** — one is present, the other isn't. Run STEP 5 first to surface what's there, then walk the user through completing the missing piece (Profile if absent → STEP 2; metrics if absent → STEP 4).

STEP 1 — Classify the workflow profile
Walk the user through the four axes. Real workflows mix them; do not force a single enum.

- **Plan stability** — `mutable` (plan changes freely), `ratchet` (additions only — compliance, security), `frozen` (no plan-shape change without explicit user OK).
- **Runtime mode** — `single` (one plan, runs as-is) vs `dual` (alternates explore / exploit; e.g. social-media trying new tactics weekly then exploiting the winner).
- **Business context accumulation** — does the workflow accumulate user-supplied business rules (audit clauses, ICP filters, risk constraints)? `yes` for Type-3-style workflows; `no` for QA suites and pure exploratory creative.
- **Improvement cadence** — how often is this workflow expected to improve? Daily / weekly / per-incident / quarterly / never (frozen).

Show your inference + reasoning + the alternative answers you considered for each axis. Ask the user to confirm.

STEP 2 — Write the Workflow Profile to builder/improve.md
Append (or replace, if a section already exists) the following section in builder/improve.md. Use `diff_patch_workspace_file` — do NOT `mkdir` via shell. Use workflow-relative paths.

```markdown
## Workflow Profile (auto-improvement framework)

- **Plan stability**: <chosen> — <one-line rationale>
- **Runtime mode**: <single | dual> — <one-line rationale; if dual, name the modes: e.g., "explore (weekly reset) / exploit (daily default)">
- **Business context**: <accumulating | none> — <one-line rationale>
- **Improvement cadence**: <chosen> — <one-line rationale>

Behavioral implications the agent should respect on every turn:
- <plan-stability implication, e.g. "Do not call replan_workflow_from_results or delete_plan_steps without explicit user approval.">
- <runtime-mode implication, e.g. "When dual: branch step behavior on the workflow's chosen runtime signal.">
- <business-context implication, e.g. "Recognize user-supplied rules in conversation and offer capture_context.">
```

STEP 3 — Set the two hard-gate fields in workflow.json
These are the only structured framework fields; they drive real behavior.

- `oversight_mode` — `manual` / `supervised` (default) / `autonomous`. Recommended defaults: deterministic + ratcheting workflow → `manual`; exploratory → `autonomous`; contextual / business-context → `supervised`.
- `decision_log_mutability` — `append_only` (default) / `append_only_strict`. Set strict ONLY for compliance / audit workflows where the decision log is forensic.

STEP 4 — Bootstrap metrics.json
Behavior depends on the profile from Step 1:

- Plan stability `mutable` + business context `none`: tell the user outcome metrics can be deferred. Track per-eval-step trajectories instead. Still propose `cost_per_run` and `run_duration_seconds` as telemetry SLOs — they're free signal and catch regressions while exploring.
- Plan stability `ratchet`/`frozen` + business context `none` (e.g. QA suite, ETL): propose 3–5 SLO-mode metrics — success-rate (floor), `cost_per_run` (ceiling), `run_duration_seconds` (ceiling), data freshness. Source: `telemetry` for cost/duration, `eval_step` for the rest.
- Business context `accumulating`: REQUIRED. Propose 3–5 outcome + rule-conformance metrics derived from success_criteria. Mix outcome metrics (mode=`target`, drive toward a value) with SLO metrics (mode=`slo`, stay above floor / below ceiling) — outcome metrics drive progress, SLOs enforce constraints. Always include `cost_per_run` and `run_duration_seconds` as telemetry SLOs.

For each proposed metric, supply id + unit + direction + mode + threshold + source. Use `propose_metric` to write each one — never shell-write `planning/metrics.json` (it's folder-guarded). Common gotchas to avoid:
- For `source.type=eval_step`, use `field=""` for the percent score, `field="score"` / `field="max_score"` for the raw values, or a structured-output key only if the eval Python emits a JSON object containing that key.
- Telemetry source: only six wired fields exist (`run.total_cost_usd`, `run.duration_seconds`, `eval.total_cost_usd`, `eval.duration_seconds`, `total.cost_usd`, `total.duration_seconds`). Other names silently return no value.

STEP 5 — REVIEW PATH (when framework is already set up)
You're auditing existing setup, not bootstrapping. Walk through these checks and surface any issues with proposed fixes. Apply nothing without user confirmation.

5.1 — **Workflow Profile sanity**
- Is the existing "## Workflow Profile" still accurate given the current plan? If the workflow has evolved (steps added/removed, mode changed) but the profile section is stale, propose updating it.
- Are the four axes filled in with rationale, or are some empty / placeholder?
- Are the behavioral implications still relevant?

5.2 — **Hard-gate fields**
- Verify `oversight_mode` and `decision_log_mutability` in workflow.json match the workflow profile. A "ratchet" stability with `oversight_mode: autonomous` is mismatched and should be flagged.

5.3 — **Metric definitions**
- Read every entry in `planning/metrics.json::metrics[]`.
- For each metric, validate: id is unique kebab.dot, unit is sensible, direction matches (e.g. `cost_per_run` should be `lower_better`), mode + threshold is consistent (target requires `target`, slo+higher_better requires `floor`, slo+lower_better requires `ceiling`).
- Does the source point at something real? `eval_step` source must reference an existing eval step id; `telemetry` source must use one of the six wired fields.
- Are there obvious gaps — success criteria from soul.md that no metric measures? Surface as coverage suggestions (don't auto-add).

5.4 — **Metric health (resolve errors)** — most important pass
Read the most recent rows of `db/metrics_history.jsonl` for each metric id. For each row with `has_value: false` and a `resolve_error`, categorize:

- "no structured output (field=X)" — the metric specifies `field=X` but the eval step emits flat output only. Two fix paths:
  (a) Update the eval step's Python to emit a structured JSON containing `X` (preferred — measures the named outcome explicitly). This is an eval-side change; recommend running `/improve-eval` with focus on that step.
  (b) Retire the metric and propose a replacement using `field=""` (percent score) or `field="score"` (raw 0-10).
- "eval step <id> not found" — id mismatch. Either restore the eval step or retire the metric and propose a new one with the correct id.
- Consistent NO VALUE without resolve_error — the eval step didn't run or produced no score. Operational coverage gap; flag for `/improve-eval`.

For each broken metric, name the metric, the resolve_error, and the recommended fix. Apply only after user confirms.

5.5 — **Unused / orphan metrics**
- Are any metrics in `planning/metrics.json` not referenced by any experiment in `experiments/active.json` or `experiments/history.jsonl`? If they're outcome metrics (i.e., genuinely meant to be tracked), that's fine — but flag any that look stale (defined long ago, never targeted, no recent history rows).

5.6 — **Telemetry SLOs present?**
- If `cost_per_run` and `run_duration_seconds` (or `total.cost_usd` / `total.duration_seconds`) aren't defined as telemetry SLOs, suggest adding them. Free signal.

After STEP 5 — Record what you reviewed and recommended in `builder/improve.md` under a "## Framework review YYYY-MM-DD" subsection so the audit trail survives the session. Append a `decisions.jsonl` entry summarizing the review.
