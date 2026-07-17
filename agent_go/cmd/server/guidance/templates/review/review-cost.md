Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write every finding into `builder/improve.html` as a **Signals / Kizuki** "Open finding" timeline entry using `data-pulse-section="signals"` and `data-module="cost_llm_time"`. For the log format, one-time old Markdown migration, and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

Anchor every judgment to the objective FIRST: read `soul/soul.md` (the workflow's goal + success criteria), recent eval reports, and cost ledgers under `costs/`. A cost cut is only "safe" if it does NOT threaten a primary success criterion or quality check; do not label a reduction safe without checking it against these.

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

REVIEW LOG: record every finding as a Signals / Kizuki "Open finding" timeline entry in builder/improve.html using `data-pulse-section="signals"` and `data-module="cost_llm_time"` (read it first if it exists, create it if it does not — newest on top) with the cost analysis, the cost drivers, every recommendation (REVIEW = recommend; do NOT apply), priority, and follow-up. Never discard findings because they fall outside a top-N cap.
