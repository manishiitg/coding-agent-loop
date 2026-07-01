Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Before writing builder/review.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown. Read the existing file first and preserve existing unresolved findings, but this cost entry itself is a report-only section, not an action queue.

MIGRATION (one-time): Check whether builder/review.md exists. If it does, read it, extract unresolved F-... findings, incorporate them into builder/review.html, then delete builder/review.md with execute_shell_command.

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

Assess four things separately:
1. Which steps, models, or phases are consuming the most cost?
2. Which spend is necessary for success versus waste from retries, too many handoffs, overly expensive models, or unnecessary evaluation breadth?
3. Which cost reductions come from tightening descriptions or reducing retries/tool calls versus changing the plan shape (merge/remove/reorder steps)?
4. Which cost reductions are safe versus risky for the objective and success criteria?

Then give a report:
- the top cost drivers with evidence
- safe reduction opportunities, clearly labeled as optional recommendations
- risky reduction ideas, clearly labeled as risky and not recommended without more evidence
- expected savings ranges when the evidence supports them
- what should continue unchanged because the spend appears necessary for success.

REVIEW LOG: append a dated **COST REPORT** entry to builder/review.html (read it first if it exists, create it if it does not). Mark the entry clearly as **REPORT ONLY — NO ACTION REQUIRED**. Include the cost analysis, top cost drivers, optional safe-reduction recommendations, risky ideas to avoid or investigate later, and spend that appears justified.

Do **not** create `F-...` finding IDs for this cost report. Do **not** add unresolved findings, action items, follow-up tasks, or close-out targets from this command. If the user later wants to act on a cost recommendation, they can ask for an optimizer/harden/replan pass; that separate action may create findings or improvement entries then.
