## regular — Single-Action Worker

A `regular` step is one agent doing one unit of work in a single execution (with a
validation-driven retry loop). It is the **single-action** type — reach for it only when a step
is **one atomic action with no verify-and-fix follow-up**. The **default** step type is
**`message_sequence`**: instead of splitting work into several smaller regular steps that hand
off context through intermediate `.md`/`.json` files, prefer one larger `message_sequence` that
does the work and then uses **verification user-message turns** to confirm everything is done
and complete — all in one shared conversation. See `get_reference_doc(kind="message-sequence")`.
Use the others for branching (`routing`), sub-agent coordination (`todo_task`), or operator
input (`human_input`).

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
- `validation_schema` — **required**; gates the step. Checks **files** (file_checks +
  json_checks) AND/OR the **db** (`db: [{sql, min_rows, max_rows, checks}]` — read-only
  queries against `db/db.sqlite`). On failure the agent retries with the failed-check
  feedback. Prefer **db checks** when the step writes its results to the db: they gate on
  the source of truth, so you don't need a hand-written output file just to validate (a
  duplicated summary file drifts from the db — e.g. a `status` that ends up null).
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
