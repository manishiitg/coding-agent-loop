Read planning/plan.json and act as a senior workflow designer reviewing this plan with the user.

Before writing builder/review.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown. Use semantic badges for design issues and recommendations. Read the existing file first to carry forward prior recommendations. Your job is to make the design BETTER — not just catch what's broken. Where review-plan asks "what's wrong?", design-flow asks "what would a thoughtful designer change?"{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

The output has three parts: (1) a visual map so the user sees what they have, (2) integrity checks (the strict broken-chain stuff), (3) **constructive recommendations** keyed to design best-practices, even when nothing is broken.

PART 1 — VISUAL MAP
Draw a dependency graph from plan.json so the user can see their workflow at a glance:

  step-1: ingest_emails  (produces: raw_emails, sender_index)
        ↓ raw_emails
  step-2: classify_intent  (consumes: raw_emails; produces: classified)
        ↓ classified
  step-3: route_by_intent  (routing: → step-4a, step-4b, step-4c)

Use ASCII or a markdown table — whichever fits the plan size. Annotate each step with its step type (regular / todo_task / human_input / routing / orphan) and declared execution mode. In Builder mode, new executable steps should stay on agentic; scripted promotion belongs to Optimizer only after the user explicitly asks, the step is highly deterministic, and 10+ scenario-covering successful runs prove stability.

PART 2 — INTEGRITY CHECKS
Flag the strict structural issues:

1. **Broken chain** — step depends on a context_output that no earlier step produces.
2. **Orphaned outputs** — step produces context_output that no later step consumes.
3. **Circular dependencies** — A depends on B depends on A.
4. **Implicit dependencies** — description references upstream data but context_dependencies doesn't list it.
5. **Type mismatches** — upstream produces JSON, downstream expects CSV; field names don't align.
6. **Missing validation** — steps that produce context_output but have no validation_schema.

Report severity (CRITICAL / WARNING / INFO) and a one-line fix per issue.

PART 3 — DESIGN RECOMMENDATIONS (the differentiator)
Apply these best-practice lenses and tell the user what would make the plan better. Each recommendation MUST cite the specific step(s) it applies to. Skip the lens if it doesn't fire.

- **Durable boundary fit**: a step is a durable workflow boundary, not a tool-call boundary. Recommend splitting only when a step mixes distinct durable outputs, validation gates, retry/failure domains, tool/security contexts, downstream contracts, persistent stores, human approvals, or routing decisions. Recommend combining adjacent steps when they share the same objective/output contract and only create pass-through artifacts or context handoff overhead.
- **Step type fit**: regular for one-shot work, todo_task for agentic loops over a list, routing for branching, human_input for explicit human judgment, orphan for reusable manual utility agents. Steps that mention "loop over each X and do Y" are often todo_task candidates that were modeled as regular. Steps that do mechanical data shaping should still be built and debugged as agentic here; note them as possible future Optimizer candidates for scripted only if the user explicitly asks later and 10+ scenario-covering successful runs prove the shape is stable.
- **Sequential agent steps that could collapse**: if steps N and N+1 share most of their context and objective, ask whether they should be one stronger step, or whether the boundary actually buys validation, retry isolation, auditability, downstream reuse, or a persistent artifact. Do not collapse boundaries that protect validation, human decisions, tool/security isolation, or reusable outputs.
- **Validation schemas as guard rails**: even if context flow is technically correct today, a validation_schema on every produces-context_output step catches drift the moment it lands — far cheaper than discovering it three steps downstream.
- **Naming**: "process_data" / "do_step" are generic. Names like "classify_emails_by_buyer_intent" make plans self-documenting and audit logs readable. If you see generic names, suggest specific ones.
- **Human-input gates**: workflows that make consequential decisions (sending messages, allocating budget, classifying medical/legal items) without a single human_input step are usually under-gated. Ask whether one belongs.
- **Business context wiring**: if a step depends on user-supplied runtime context, preferences, constraints, examples, ICP filters, approval rules, or style requirements, recommend storing that context in `knowledgebase/context/context.md`, setting `knowledgebase_access=read` or `read-write` on the affected step, and adding a sentence to that step's description naming the relevant context section/path. Do not leave the dependency implicit in chat memory.
- **Group separation**: if multiple variable groups exist (e.g. "saurabh" / "anika"), check whether the plan branches on group identity in places where it shouldn't (a step description that says "for Saurabh, do X, for Anika, do Y" indicates a routing step is missing).
- **Output surface bloat**: a step with 5+ context_outputs is hard for downstream consumers to navigate. Recommend splitting into smaller steps or wrapping outputs into a single structured object.
- **Mechanical transforms over LLM calls**: 2+ sequential regular steps that just reshape data (filter / aggregate / map fields) may deserve a clearer single agentic transform step now. Optimizer can later consider scripted only if the user explicitly asks and 10+ scenario-covering successful runs prove the transform is stable.
- **No-op feedback loop**: workflows with consequential decisions and no human_input may be under-gated. If outcome measurement is missing or weak, flag it as an Optimizer follow-up; Builder should not draft evaluation_plan.json in this command.

For each recommendation, give:
  - **What's there now** (one sentence quoted from plan).
  - **What to consider** (the better shape, with concrete example).
  - **Why** (which best-practice it serves).

PART 4 — TOP 3
Close with a "if you change three things, change these" list — the highest-impact recommendations from PART 3, prioritized.

REVIEW LOG: append a dated entry to builder/review.html (read it first if it exists, create it if it does not). Include: what was reviewed, integrity issues by severity, the design recommendations grouped by lens, the top-3 list, items flagged for follow-up. Mark this as REVIEW (recommend; do NOT apply).
