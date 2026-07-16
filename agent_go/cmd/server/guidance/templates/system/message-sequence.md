## MESSAGE SEQUENCE — SAME-CONTEXT CONVERSATIONAL WORK

Use `message_sequence` for one persistent conversation where later turns need the earlier turns' reasoning, tool output, critique, or context. The step `description` is turn 0; `items[]` are turns 1..N.

Supported item types:

- `user_message`: one focused follow-up instruction.
- `foreach`: one templated follow-up per row from a read-only query against `db/db.sqlite`.
- `prevalidation`: a deterministic backend validation gate with corrective feedback sent to the same conversation.

`type: "code"` was removed in workflow contract v1.0.10. Deterministic code must be a standalone `regular` step configured with `declared_execution_mode: "scripted"`, with its script at `learnings/<step-id>/main.py`. Connect conversational and scripted steps through explicit `context_dependencies`, `context_output`, database contracts, and validation.

Preferred split when deterministic work is needed:

```text
message_sequence: collect-and-clarify
  -> regular scripted: normalize-inputs
  -> message_sequence: review-normalized-results
```

Do not hide durable computation, side effects, retries, or file handoffs inside conversation state.

## WHEN TO USE IT

Use it when:

- Turns read the same upstream context.
- Critique and correction should happen in the same specialist conversation.
- The unit has one tightly coupled outcome and should fail/retry together.
- A todo_task route needs a specialist that can be re-entered during the same workflow run.

Do not use it when:

- A phase has an independent artifact, validation gate, retry domain, model, credential, or downstream consumer.
- Work is deterministic code; use a scripted regular step.
- The workflow needs deterministic branching; use `routing`.
- Work should be delegated independently; use `todo_task` routes.

## MEMORY

- A top-level message_sequence runs its fixed item queue once.
- A message_sequence inside a todo_task route can be re-entered during the same workflow run.
- Route memory is in-memory only. It does not survive process restart or a later workflow run.
- `message_sequence_restart=true` starts a clean route conversation when prior context is stale or contaminated.
- `session.json` is an observability record, not resume state.

## WRITE ACCESS

Items can read execution outputs, `db/`, knowledgebase, learnings, and soul. Writes are disabled by default and must be declared per item:

- database writes: `"write_access": {"db": true}` or `"kind": "db"`
- knowledgebase writes: `"write_access": {"knowledgebase": true}` or `"kind": "knowledgebase"`
- learning writes: `"write_access": {"learnings": true}` or `"kind": "learning"`

Write access is folder-level. Per-file path lists are rejected. Use learning writes sparingly; normal step-level learning runs after the complete step.

## PREVALIDATION

Place a `prevalidation` item immediately after the conversational turn whose artifacts it checks. On a normal validation failure, the runtime sends concrete failures back to the same conversation for correction and retries the gate. Infrastructure failures stop the step.

```json
{
  "id": "verify-output",
  "type": "prevalidation",
  "validation_schema": {
    "files": [
      {"file_name": "output/result.json", "required": true, "validation_type": "json"}
    ]
  }
}
```

Use deterministic validation for artifacts and schemas. Use a user-message critique turn for subjective review.

## FOREACH

Use `foreach` when every selected database row must get one conversational turn:

```json
{
  "id": "review-findings",
  "type": "foreach",
  "source_sql": "SELECT id, summary FROM findings WHERE status='open' ORDER BY id",
  "message": "Review finding {{"{{.id}}"}}: {{"{{.summary}}"}}",
  "max_iterations": 50
}
```

`source_sql` must be read-only. Each result row is bound to `.` in the Go template. Add one static prevalidation after the loop when the aggregate result needs a gate.

## ROUTE PATTERNS

Route sub-agents can be `regular` for stateless one-off work or `message_sequence` for a stateful specialist conversation. Use these patterns when designing or repairing todo_task predefined routes.

For a todo_task route, use `message_sequence` when the orchestrator should preserve specialist memory. As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent: Normal repeated calls reuse the route conversation and each call is delivered as a re-entry user message. Set `message_sequence_restart=true` to restart only when the prior conversation is stale, wrong, or contaminated.

## MESSAGE SEQUENCE ROUTE PATTERNS

- Stateful specialist: re-enter one route for follow-up work.
- Test/fix loop: validate externally, then re-enter the specialist with concrete failures.
- Maker/reviewer: keep creation and independent review in separate routes.
- Panel: separate domain specialists coordinated by a todo_task orchestrator.
- Clean-room retry: restart a contaminated specialist route.
- Human feedback: send approved operator feedback into the same route conversation.

## AUTHORING RULES

- Write the real opening instruction in `description`.
- Keep each user-message item focused on one outcome.
- Use explicit durable files or DB rows for cross-step handoff.
- Declare item write access before execution.
- Use prevalidation for deterministic acceptance checks.
- Keep code, its retries, permissions, logs, and costs in standalone scripted regular steps.
- Never add a legacy code item. If an old plan contains one, require the v1.0.10 workflow preflight migration.
