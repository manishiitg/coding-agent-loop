## regular — Scripted Deterministic Worker

A `regular` step is the scripted boundary for one deterministic unit of work. It owns one
coherent output and deterministic final gate and runs through the saved `main.py` path.
Do not create an agentic regular step: every new conversational or judgment-heavy step uses
**`message_sequence`**, even when it needs only one work turn. Persisted non-scripted regular
steps are normalized to a one-turn message sequence at runtime; they never use the removed
direct regular-agent path. See `get_reference_doc(kind="message-sequence")`.
Use the others for branching (`routing`), sub-agent coordination (`todo_task`), or operator
input (`human_input`).

## When to use

- Deterministic, self-contained work: fixed API/SDK calls, CLI commands, data fetching, known pagination, parse, normalize, transform, write, and mechanically verify. Declare these steps `scripted` from initial design and batch related calls that share one source/auth/retry/output contract.
- One clear deterministic objective expressible as a `description` plus a `validation_schema`.
- A coherent scripted boundary inside the **Linear Pipeline** pattern (see `get_reference_doc(kind="workflow-patterns")`), not one step per pipeline action.

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

## Execution mode

- **Scripted / code-execution mode** is the only mode for new regular steps. Create one with `add_scripted_step`; the internal plan type remains `regular`. The builder authors a `main.py` saved under
  `learnings/{step-id}/` and re-runs it on later runs (scripted fast path). Use for
  deterministic, repeatable execution. No run-history threshold is required to declare an obviously deterministic step scripted; 10+ representative successful runs are required only before `lock_code=true` freezes it. See `get_reference_doc(kind="code-authoring")`.
- Judgment, adaptive discovery, ambiguous live evidence, and browser/UI work use `message_sequence`.

Preferred data shape: `regular scripted fetcher(s) → message_sequence processor`. Fetchers own credentials, calls, retries/rate limits, provenance, freshness, idempotency, response parsing, and authoritative DB/file output. The message sequence reads that output and owns semantic analysis, synthesis, critique, and repair.

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
