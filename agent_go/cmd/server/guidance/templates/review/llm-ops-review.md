# STANDALONE LLM AND OPERATIONS REVIEW

Run the same low-frequency read-only LLM/Ops review used by Pulse. Do not change
models, tiers, fallbacks, schedules, notification recipients, backup, publish,
or credentials in this command.{{if .Focus}}

Focus especially on: {{.Focus}}.{{end}}{{if .RunFolder}}

Use `{{.RunFolder}}` as the primary run folder.{{end}}

1. Load `get_reference_doc(kind="post-run-monitor")`,
   `get_reference_doc(kind="llm-selection")`, and
   `get_reference_doc(kind="review-improve-log")`.
2. Inspect resolved workflow/step/eval LLM configuration, actual model/tier use,
   fallbacks, matching cost and timing evidence, missing/unpriced buckets,
   retained `efficiency_or_coaching` findings, workflow version, and current
   backup/publish/notify readiness. Also inspect the current trustworthy Goal
   verdict and material success-criterion evidence. Use actual retained evidence,
   not provider assumptions or generic best practices.
3. Launch exactly one generic reviewer with a prompt beginning
   `READ-ONLY REVIEW`. It must not edit files or config, create questions,
   publish, notify, run the workflow, call Pulse module-state tools, or launch
   another agent.
4. Require a compact result grouped by `cost saving`, `quality`, `reliability`,
   and `setup`. Every recommendation needs current state, exact suggestion,
   expected benefit, risk, and evidence. Separate missing telemetry from true
   optimization opportunities. If a material goal criterion is below target,
   forbid tier/model downgrades for outcome-bearing, reasoning, diagnostic,
   recovery, eval, and verification steps. A downgrade is eligible only for a
   deterministic non-bottleneck step with representative evidence proving
   quality-equivalent output and no downstream outcome loss; label it as an
   approval-required reversible trial. Missing evidence means keep the tier.
5. Validate and deduplicate the result against `builder/improve.html`. As the
   parent, refresh one compact LLM & operations review area with
   `data-pulse-section="signals" data-module="llm_ops_review"` in that HTML. Do not
   apply recommendations or create approval cards in this read-only command.

Finish with a short executive summary followed by every evidence-backed
recommendation in severity order. Identify which exact changes require user
approval before `/pulse-fixer` can apply them. Do not truncate the result to a
Top 3.
