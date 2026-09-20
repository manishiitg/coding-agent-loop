# SPECIALIZE STRATEGY AUDITOR AND GOAL ADVISOR

Propose two durable workflow-specific review lenses for owner approval. Do not
change the canonical reviewer roles, the workflow goal, plan, permissions, or
any workflow artifact during proposal generation.{{if .Focus}}

Use this owner context when shaping the proposal: {{.Focus}}.{{end}}

1. First load the complete base contracts with
   `read_skill(skills=[{"name":"builder-reference","path":"references/strategy-auditor.md"}])`
   and
   `read_skill(skills=[{"name":"workflow-commands","path":"references/goal-advisor.md"}])`.
   Then read `workflow.json`, `soul/soul.md`, the current plan and step config,
   report/evaluation contracts, representative retained runs, and bounded
   read-only workflow DB evidence. If `workflow.json` already contains
   `pulse.advisor_specialization`, treat it as the current version to improve,
   not as an immutable constraint.
2. Apply a **DELTA-ONLY CONTRACT**. The specialization must contain only useful
   knowledge the corresponding base contract does not already provide. Do not
   restate generic reviewer method such as reading plans/runs/DB, tracing a
   causal chain, comparing evidence, checking provenance/freshness, segmenting,
   falsifying alternatives, measuring cost/time, asking for approval, or
   defining bounded experiments, success measures, risks, stop conditions, and
   rollback. The base prompts already own that procedure.
3. Retain only durable workflow/domain additions such as:
   - concrete evidence sources or observation vantage points and how their
     agreement, disagreement, absence, or trust should be interpreted;
   - domain-specific false-positive and false-negative traps;
   - stable domain entities, surfaces, cohorts, outcomes, invariants,
     guardrails, and consequential tradeoffs a general reviewer may miss;
   - for Strategy Auditor, domain semantics needed to judge the workflow's
     *current strategic shape*;
   - for Goal Advisor, credible domain opportunity spaces and counterfactuals
     outside the current shape, without choosing the winning solution in
     advance.
4. Run this specificity filter on every sentence before saving it:
   - If it would still be useful for an unrelated workflow after replacing the
     domain nouns with “workflow”, “source”, or “item”, remove it.
   - If the matching base contract already asks for it, remove it.
   - Every retained paragraph must name a concrete stable domain concept and
     explain its special meaning, evidence semantics, or tradeoff. Merely naming
     a domain noun inside generic advice does not pass.
   Use the minimum text needed. Produce exactly two compact Markdown texts, not
   two restatements of the base prompts.
5. Keep both texts reusable across runs. Exclude current bugs, open findings,
   temporary incidents, stale status, preselected conclusions, copied logs,
   recommended final solutions, and instructions to bypass approval or safety.
   They specialize perspective only. The canonical reviewer contract and the
   current owner-approved soul/plan always win on conflict.
6. Store the exact proposal in one durable decision using
   `create_human_input_request` with:
   - `workspace_path` set to the current workflow,
   - `source="pulse"`, priority `medium`, and a unique
     `input_id="advisor-specialization-<UTC timestamp>"`,
   - question: `Activate these workflow-specific Strategy Auditor and Goal Advisor lenses?`,
   - options `activate`, `revise`, and `reject`, with free text allowed,
   - context in this exact shape, with the complete text (not a summary):

     `Proposal:`

     `Specialize Strategy Auditor and Goal Advisor for this workflow. This changes review context only; it does not modify the goal or plan.`

     `Strategy Auditor specialization:`

     `<complete Strategy Auditor Markdown>`

     `Goal Advisor specialization:`

     `<complete Goal Advisor Markdown>`

     The two Markdown bodies must not repeat either section-heading phrase.
7. Do not activate a new proposal in the same turn. After the owner selects
   `activate`, call
   `update_workflow_config(advisor_specialization_approval_input_id="<decision id>")`.
   That tool resolves the exact approved text, writes both lenses together to
   `workflow.json`, versions the change, and consumes the decision. `revise`
   means generate a new proposal (using the answer note as focus); `reject`
   leaves the current specialization unchanged.

Finish by showing both proposed texts and the decision id. Do not write
`workflow.json` directly and do not claim the lenses are active until the
activation tool succeeds.
