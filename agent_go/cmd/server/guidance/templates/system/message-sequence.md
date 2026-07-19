## MESSAGE SEQUENCE — SAME-CONTEXT CONVERSATIONAL WORK

Use `message_sequence` for one persistent conversation where later turns need the earlier turns' reasoning, tool output, critique, or context. Design one large sequence per coherent shared-context span. The step `description` is turn 0; `items[]` are turns 1..N.

The default shape is `[complete the whole shared-context span] → [re-open authoritative evidence and prove every criterion] → [repair gaps and double-check]`, followed by the top-level deterministic validation gate. Require run-specific proof/provenance in the output so validation cannot pass a stale or self-asserted success. Do not create separate workflow steps for these checks.

Use multiple large sequences when contexts should not be shared: different credentials/security exposure, independently rerunnable outputs or failure domains, clean-room reviewer independence, human/routing boundaries, or unrelated context that would distract or contaminate the next agent. The builder should decide this from workflow semantics and be able to state the isolation reason.

Supported item types:

- `user_message`: one focused follow-up instruction.
- `foreach`: one templated follow-up per row from a read-only query against `db/db.sqlite`.
- `prevalidation`: a deterministic backend validation gate with corrective feedback sent to the same conversation.

`type: "code"` was removed in workflow contract v1.0.10. Deterministic code must be a standalone `regular` step configured with `declared_execution_mode: "scripted"`, with its script at `learnings/<step-id>/main.py`. Connect conversational and scripted steps through explicit `context_dependencies`, `context_output`, database contracts, and validation.

Preferred split when deterministic data is needed:

```text
regular scripted: fetch-and-normalize-authoritative-data
  -> message_sequence: analyze-verify-and-repair-from-fetched-data
```

Batch related API/SDK calls or CLI commands into the fetcher when they share credentials, retry/rate-limit behavior, source, and output contract. The fetcher owns pagination, stable parsing, provenance/freshness, idempotency, fail-closed errors, and deterministic DB/file validation. Do not use one step per endpoint, and do not spend sequence turns reissuing known requests or parsing stable response shapes.

When the request itself needs judgment, use `message_sequence: decide-and-write-request-spec -> regular scripted: execute-request-spec -> message_sequence: interpret-and-verify-result`.

Do not hide durable computation, side effects, retries, or file handoffs inside conversation state.

## WHEN TO USE IT

Use it when:

- Turns read the same upstream context.
- Critique and correction should happen in the same specialist conversation.
- The unit has one tightly coupled outcome and should fail/retry together.
- A todo_task route needs a specialist that can be re-entered during the same workflow run.

Do not use it when:

- A phase has an independent artifact, independently rerunnable validation/failure domain, model, credential, downstream consumer, or context that should be isolated.
- Work is deterministic code; use a scripted regular step.
- Work is a fixed API/SDK call, CLI command, data fetch, stable parse/normalize operation, or mechanical write; use a scripted regular step and consume its durable result here.
- The workflow needs deterministic branching; use `routing`.
- Work should be delegated independently; use `todo_task` routes.

## MEMORY

- A top-level message_sequence runs its fixed item queue once.
- A message_sequence inside a todo_task route can be re-entered during the same workflow run.
- Route memory is in-memory only. It does not survive process restart or a later workflow run.
- `message_sequence_restart=true` starts a clean route conversation when prior context is stale or contaminated.
- `session.json` is an observability record, not resume state.

## WRITE ACCESS

Items inherit the step-level DB, knowledgebase, and learnings permissions, matching a regular execution step. Usually no item-level access declaration is needed.

Use a non-empty `write_access` object, or `kind`, only when one turn should be narrowed to selected stores:

- database writes: `"write_access": {"db": true}` or `"kind": "db"`
- knowledgebase writes: `"write_access": {"knowledgebase": true}` or `"kind": "knowledgebase"`
- learning writes: `"write_access": {"learnings": true}` or `"kind": "learning"`

An item override can narrow but never exceed the step-level permissions. Write access is folder-level and per-file path lists are rejected. Use direct learning writes sparingly; normal step-level learning runs after the complete step.

**Where a step can durably write (the hard allow-list).** The sandbox opens only these durable locations for a step, and denies everything else ("operation not permitted"):

- `db/db.sqlite` and `db/assets/` — structured rows and durable **files** of any format (PDF, image, CSV, JSON, txt, zip). A downloaded or generated file that later steps or the builder must reach goes in `db/assets/` with a reference row in `db.sqlite`. This is the ONLY step-writable home for an arbitrary file.
- `knowledgebase/notes/` — workflow-discovered narrative facts (KB direct-write only).
- `learnings/_global/` — reusable HOW-to-run knowledge.
- the step's own execution folder + `Downloads/` — volatile per-run scratch (wiped on re-run).

`docs/`, `knowledgebase/context/`, other `knowledgebase/` subfolders, and any custom top-level folder are **builder-only or denied** — do not route a step's durable file there.

## PREVALIDATION

The step-level `validation_schema` is always the final gate. The runtime runs it automatically after the configured work turns and before synthetic learning/knowledge closing turns. On a normal validation failure, it sends concrete failures back to the same conversation for correction and retries the gate. Infrastructure failures stop the step.

Add an explicit `prevalidation` item only when an intermediate artifact must pass before later work turns proceed. If the final configured item already validates the same step-level schema, the runtime does not add a duplicate final gate.

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

`source_sql` must be read-only. Each result row is bound to `.` in the Go template. The step-level `validation_schema` automatically gates the final aggregate result; add a static prevalidation after the loop only when later items must not run unless that intermediate aggregate passes.

## ROUTE PATTERNS

Conversational route sub-agents use `message_sequence`, including stateless one-turn work. Use `regular` only for an explicitly scripted deterministic route. Use these patterns when designing or repairing todo_task predefined routes.

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
- Make turn 0 own the whole shared-context outcome; do not turn routine phases into separate items.
- Require run-specific proof/provenance, then add a turn that re-opens authoritative evidence and proves every criterion, followed by repair and double-checking.
- Keep each user-message item focused on one outcome.
- Use explicit durable files or DB rows for cross-step handoff.
- Declare item write access before execution.
- Put the final deterministic acceptance contract in the step-level `validation_schema`; use explicit prevalidation items only for intermediate checks.
- Create another large sequence only when its context should be intentionally isolated, and record the boundary rationale in the plan description/review.
- Keep code, its retries, permissions, logs, and costs in standalone scripted regular steps.
- Never add a legacy code item. If an old plan contains one, require the v1.0.10 workflow preflight migration.
