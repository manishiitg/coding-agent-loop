Run review_plan() to critically analyze the current workflow plan.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Challenge every decision: step boundaries, step types, execution modes, context flow, validation coverage, portability, and whether choices are justified by the objective and success criteria. Report findings by severity — don't just summarize, identify what's weak, risky, or unjustified.

This is a plan/design review. Use review_workflow_results() when the question is whether a real run is actually achieving the goal and whether eval measures that properly.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what was reviewed, the main findings ordered by severity, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.
