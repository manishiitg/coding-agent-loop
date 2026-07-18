Run review_workflow_timing() to analyze where workflow time is actually going and how to make it faster.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

The active Workshop turn is the parent coordinator. Treat
`review_workflow_timing` as a read-only specialist. It must not change files,
models, tiers, config, questions, module state, or `builder/improve.html`. Load
`get_reference_doc(kind="review-improve-log")` for the shared reviewer/writer
boundary. Do not load `html-output` or the HTML skeleton for the specialist,
inspect Pulse CSS, or ask it to format cards.

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

Return a compact non-HTML review packet with `module=cost_llm_time`, `verdict`,
`next_check`, and ordered findings. Every finding includes a stable `finding_id`,
`target_key`, severity, plain-language summary, exact evidence, bounded
`recommended_fix`, verification, and `user_judgment_required` with reason.
Preserve every finding; never discard findings because they fall outside a
top-N cap.

PARENT CLOSE-OUT: validate and deduplicate the complete packet, then make one
bounded newest-first update to `builder/improve.html` with **Signals / Kizuki**
cards using `data-pulse-section="signals"` and
`data-module="cost_llm_time"`. Record recommendations as REVIEW only; do not
apply them or make the specialist format HTML.
