Run review_workflow_timing() to analyze where workflow time is actually going and how to make it faster.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Before writing builder/review.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown. Use .badge.fail for CRITICAL findings, .badge.warn for WARNING, .badge.pass for resolved. Read the existing file first to carry forward unresolved findings.

MIGRATION (one-time): Check whether builder/review.md exists. If it does, read it, extract unresolved F-... findings, incorporate them into builder/review.html, then delete builder/review.md with execute_shell_command.

{{if .RunFolder}}Use the selected run folder "{{.RunFolder}}" as the primary evidence set.{{else}}If a meaningful prior run exists, use it as evidence; otherwise find the latest meaningful run first.{{end}}

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

REVIEW LOG: append a dated entry to builder/review.html (read it first if it exists, create it if it does not) with the timing analysis, the top bottlenecks, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.

**Finding IDs.** Every distinct bottleneck or recommendation gets a stable id of the form `F-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan the file for today's highest existing sequence and continue from there; never reuse an id. Format each finding line as `- [F-YYYY-MM-DD-NNN] <severity>: <step-id or "plan-shape"> — <finding>` so close-out edits performed later by `/improve-*` (or by chat-driven fixes) can target the exact entry.
