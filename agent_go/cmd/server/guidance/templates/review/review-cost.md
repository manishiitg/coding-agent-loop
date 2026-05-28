Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Before writing builder/review.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown. Use .badge.fail for CRITICAL findings, .badge.warn for WARNING, .badge.pass for resolved. Read the existing file first to carry forward unresolved findings.

MIGRATION (one-time): Check whether builder/review.md exists. If it does, read it, extract unresolved F-... findings, incorporate them into builder/review.html, then delete builder/review.md with execute_shell_command.

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

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

**Finding IDs.** Every distinct cost driver or recommendation gets a stable id of the form `F-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id. Format each finding line as `- [F-YYYY-MM-DD-NNN] <severity>: <step-id or "plan-shape"> — <finding>` so close-out edits performed later by `/improve-*` (or by chat-driven fixes) can target the exact entry.
