## regular — Single-Step Worker

A `regular` step is one agent doing one unit of work in a single execution (with a
validation-driven retry loop). It is the **default** step type — reach for it unless
the task needs branching (`routing`), sub-agent coordination (`todo_task`), same-context
ordered turns / re-entrant conversation (`message_sequence`), or operator input (`human_input`).

## When to use

- Deterministic, self-contained work: fetch, parse, transform, write, verify.
- One clear objective expressible as a `description` plus a `validation_schema`.
- The building block of the **Linear Pipeline** pattern (see `get_reference_doc(kind="workflow-patterns")`).

If the work fans out over items, branches on a decision, needs several turns that
share one conversation, or needs a specialist that remembers across calls, it is
**not** a regular step — see the redirects below.

## Anatomy

- `description` — the executable instruction/prompt for the step agent, not metadata. Resolved variable values are available as `$VAR_*`.
- `context_dependencies` → `context_output` — forward-only context flow between steps.
- `validation_schema` — **required**; gates the step (file checks + json_checks). On
  failure the agent retries with the failed-check feedback.
- Stores: reads soul / db / knowledgebase / learnings per access; writes its own step
  folder + `db/`, plus knowledgebase notes / learnings when access is read-write
  (learnings writes happen in a dedicated post-step turn — see `get_reference_doc(kind="stores")`).

## Execution modes

- **Tool/agent mode** (default): the agent runs tools/shell to produce its outputs.
- **Code-execution / learn_code**: the agent authors a `main.py` saved under
  `learnings/{step-id}/` and re-runs it on later runs (scripted fast path). Use for
  deterministic, repeatable compute. See `get_reference_doc(kind="code-authoring")`.

## When NOT to use (redirects)

- Branching on a decision or run flag → **`routing`**.
- Coordinating ≥2 specialized sub-agents, or dynamic per-item work → **`todo_task`**.
- Same-context ordered turns, a stateful conversation, self-validation/grounding
  gate, or stepping through a db array row-by-row → **`message_sequence`**
  (incl. its `foreach` item).
- Pausing for human approval/selection → **`human_input`**.

## Anti-patterns

- Cramming multiple durable outputs into one step — split at output / store /
  failure-domain boundaries.
- Narrative branching in the description ("if X do A else B") — use a `routing` step.
- A regular step that just enumerates a list and processes each item — if the list is
  a db array, use a `foreach` (see `workflow-patterns` #10); if each item needs
  sub-agents, use `todo_task`.
- Missing or weak `validation_schema` — every step needs one strong enough that a
  bad/absent output fails it.
