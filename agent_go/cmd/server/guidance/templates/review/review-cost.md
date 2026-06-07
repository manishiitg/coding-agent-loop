Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write findings to `builder/review.html`. For the log format + badges, the one-time `.md → .html` migration, and the `F-…` finding-id scheme, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

Anchor every judgment to the objective FIRST: read `soul/soul.md` (the workflow's goal + success criteria) and `planning/metrics.json` (+ recent `db/metrics_history.jsonl` for trend) — especially the `cost_per_run`/`total.cost_usd` metrics. A cost cut is only "safe" if it does NOT threaten a primary success or quality metric; do not label a reduction safe without checking it against these.

Assess four things separately:
1. Which steps, models, or phases are consuming the most cost?
2. Which spend is necessary for success versus waste from retries, too many handoffs, overly expensive models, or unnecessary evaluation breadth?
3. Which cost reductions come from tightening descriptions or reducing retries/tool calls versus changing the plan shape (merge/remove/reorder steps)?
4. Which cost reductions are safe versus risky for the objective and success criteria?

Then give:
- the top cost drivers with evidence
- the best description/prompt changes
- the best plan changes to reduce unnecessary steps or handoffs
- the best model/tool/config changes
- the top next actions, with expected savings and risk to success criteria.

REVIEW LOG: append a dated entry to builder/review.html (read it first if it exists, create it if it does not) with the cost analysis, the top cost drivers, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.
