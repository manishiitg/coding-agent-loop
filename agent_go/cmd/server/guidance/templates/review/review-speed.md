Run review_workflow_timing() to analyze where workflow time is actually going and how to make it faster.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write findings into `builder/improve.html` as "Open finding" timeline entries. For the log format, one-time old Markdown migration, and how open findings are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

Anchor every judgment to the objective FIRST: read `soul/soul.md` (the workflow's goal + success criteria), recent eval reports, and timing summaries under `runs/<run_folder>/logs/<step-id>/execution/`. A speedup is only "safe" if it does NOT threaten a primary success criterion or quality check; do not label a change safe without checking it against these.

Assess four things separately:
1. What is the overall workflow wall-clock, and what is the biggest bottleneck class: LLM latency, tool latency, orchestration overhead, or plan shape?
2. Which groups and steps are consuming the most time, with the split between total time, LLM time, tool time, and unexplained overhead?
3. Which speedups come from tightening descriptions or reducing tool thrash versus changing the plan shape (merge/remove/reorder steps)?
4. Which speedups are safe versus risky for the objective and success criteria?

Then give:
- the top bottlenecks with evidence
- the best description/prompt changes
- the best plan changes to reduce handoffs or unnecessary steps
- the best model/tool/config changes
- the top next actions, with expected impact and risk to success criteria.

REVIEW LOG: record findings as "Open finding" timeline entries in builder/improve.html (read it first if it exists, create it if it does not — newest on top) with the timing analysis, the top bottlenecks, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.
