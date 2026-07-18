Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

The active Workshop turn is the parent coordinator. Treat
`review_workflow_costs` as a read-only specialist. It must not change files,
models, tiers, config, questions, module state, or `builder/improve.html`. Load
`get_reference_doc(kind="review-improve-log")` for the shared reviewer/writer
boundary. Do not load `html-output` or the HTML skeleton for the specialist,
inspect Pulse CSS, or ask it to format cards.

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
