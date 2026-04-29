Run review_workflow_results() to judge actual workflow outcomes, not just the plan.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

Assess three things separately:
1. Is the workflow actually achieving the stated objective?
2. Which success criteria are met, partial, unmet, or still unknown?
3. Does the evaluation plan/report actually measure the objective and success criteria properly, or is it giving false confidence?

For each success criterion, show:
- status: met / partial / unmet / unknown
- the strongest run evidence
- whether eval measures it directly, indirectly, weakly, or not at all

Then give:
- an overall verdict on goal achievement
- an overall verdict on evaluation quality
- the most important workflow gaps
- the most important eval gaps
- the top next actions, clearly separated into workflow fixes vs eval fixes.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with the goal/eval verdict, the main gaps ordered by severity, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.
