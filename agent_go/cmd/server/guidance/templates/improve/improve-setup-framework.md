Set up the auto-improvement framework on this workflow. One-time configuration: write a "## Workflow Profile" section into builder/improve.md (the durable behavioral metadata the agent reads on every turn), set the two hard-gate fields in workflow.json (oversight_mode + decision_log_mutability), and bootstrap an initial metrics.json so /improve-* and the experiment loop have something concrete to work with.{{if .Focus}}

Focus / hints from user: {{.Focus}}{{end}}

DISCOVERY (read-only)
1. Read workflow.json. Note any existing oversight_mode / decision_log_mutability.
2. Read builder/improve.md if present — note any existing "## Workflow Profile" section.
3. Read soul/soul.md to extract the workflow's objective and success_criteria.
4. Read planning/plan.json — note the steps, their types, and overall structure (frozen plan vs in flux vs explore/exploit).
5. Read evaluation/evaluation_plan.json if present — eval steps will be the natural source for many starter metrics.
6. Read <workflow>/planning/metrics.json if present.
7. Read runs/ to see how mature the workflow is.

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
- <runtime-mode implication, e.g. "When dual: read metrics.json for active_mode; honor it in step branching.">
- <business-context implication, e.g. "Recognize user-supplied rules in conversation and offer capture_context.">
```

STEP 3 — Set the two hard-gate fields in workflow.json
These are the only structured framework fields; they drive real behavior.

- `oversight_mode` — `manual` / `supervised` (default) / `autonomous`. Recommended defaults: deterministic + ratcheting workflow → `manual`; exploratory → `autonomous`; contextual / business-context → `supervised`.
- `decision_log_mutability` — `append_only` (default) / `append_only_strict`. Set strict ONLY for compliance / audit workflows where the decision log is forensic.

STEP 4 — Bootstrap metrics.json
Behavior depends on the profile from Step 1:

- Plan stability `mutable` + business context `none`: tell the user outcome metrics can be deferred. Track per-eval-step trajectories instead. **But still propose the two universal telemetry SLOs below** — they're free and catch cost/runtime regressions while exploring.
- Plan stability `ratchet`/`frozen` + business context `none` (e.g. QA suite, ETL): propose 3–5 SLO-mode metrics — success-rate (floor), `cost_per_run` (ceiling), `run_duration_seconds` (ceiling), data freshness. Source: `telemetry` for cost/duration, `eval_step` for the rest.
- Business context `accumulating`: REQUIRED. Propose 3–5 outcome + rule-conformance metrics derived from success_criteria, **plus the two universal telemetry SLOs**. Mix outcome metrics (mode=`target\
