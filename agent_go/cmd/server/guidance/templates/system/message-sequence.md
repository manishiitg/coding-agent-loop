**Saved-code paths:** Read `workflow.json.code_layout_version` first. In this reference, `<script-dir>` means `code/<step-id>` for version 1, or `learnings/<step-id>` for absent/zero (legacy). Resolve the placeholder before using a path; never infer the version from folders or migrate an existing workflow implicitly. Version 1 executes and repairs canonical source directly, with shared helpers under `WORKFLOW_CODE_ROOT`; only legacy workflows copy code into runs and save it back.

## MESSAGE SEQUENCE — SAME-CONTEXT CONVERSATIONAL WORK

Use `message_sequence` for one persistent conversation where later turns need the earlier turns' reasoning, tool output, critique, or context. Design one large sequence per coherent shared-context span. The step `description` is turn 0; `items[]` are turns 1..N.

The default shape is `[complete the whole shared-context span] → [re-open authoritative evidence and prove every criterion] → [repair gaps and double-check]`, followed by the top-level deterministic validation gate. Require run-specific proof/provenance in the output so validation cannot pass a stale or self-asserted success. Do not create separate workflow steps for these checks.

Use multiple large sequences when contexts should not be shared: different credentials/security exposure, independently rerunnable outputs or failure domains, clean-room reviewer independence, human/routing boundaries, or unrelated context that would distract or contaminate the next agent. The builder should decide this from workflow semantics and be able to state the isolation reason.

Supported item types:

- `user_message`: one focused follow-up instruction.
- `foreach`: one templated follow-up per row from a read-only query against `db/db.sqlite`.
- `prevalidation`: a deterministic backend validation gate with corrective feedback sent to the same conversation.

`type: "code"` was removed in workflow contract v1.0.10. Deterministic code must be a standalone `regular` step — the type alone makes it scripted; create it with `add_scripted_step`, or move existing conversational work there with `change_step_type` — with its script at `<script-dir>/main.py`. Connect conversational and scripted steps through explicit `context_dependencies`, `context_output`, database contracts, and validation.

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
- An orchestrator route needs a specialist that can be re-entered during the same workflow run.

Do not use it when:

- A phase has an independent artifact, independently rerunnable validation/failure domain, model, credential, downstream consumer, or context that should be isolated.
- Work is deterministic code; use a scripted regular step.
- Work is a fixed API/SDK call, CLI command, data fetch, stable parse/normalize operation, or mechanical write; use a scripted regular step and consume its durable result here.
- The workflow needs deterministic branching; use `branch` (small in-flow
  decision) or `routing` (major sub-workflow fork).
- Work should be delegated independently; use `orchestrator` routes.

## MEMORY

- A top-level message_sequence runs its fixed item queue once.
- A message_sequence inside an orchestrator route can be re-entered during the same workflow run.
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

**Persistent stores and scratch space (the hard allow-list).** Access still depends on the current step/item grants. DB rows use managed tools; file grants do not authorize opening the live database:

- DB rows via `mutate_workflow_db` (never direct SQLite access from an agentic step); `db/assets/` for durable **files** of any format (PDF, image, CSV, JSON, txt, zip). A downloaded or generated file that later steps or the builder must reach goes in `db/assets/` with a reference row in `db.sqlite`. This is the ONLY step-writable home for an arbitrary file.
- `knowledgebase/notes/` — workflow-discovered narrative facts (KB direct-write only).
- `learnings/_global/` — reusable HOW-to-run knowledge.
- the step's own execution folder + `Downloads/` — volatile per-run scratch (wiped on re-run).

`docs/`, `knowledgebase/context/`, other `knowledgebase/` subfolders, and any custom top-level folder are **builder-only or denied** — do not route a step's durable file there.

## PREVALIDATION

The step-level `validation_schema` is the final gate when the step declares one. The runtime runs it automatically after the configured work turns and before synthetic learning/knowledge closing turns. On a normal validation failure, it sends concrete failures back to the same conversation for correction and retries the gate. Infrastructure failures stop the step.

**Validate on what the step actually produces — prefer the db.** Choose the schema by the step's real output, not by habit:

- Produces a structured JSON the next step reads → a `files` rule on that file, and declare the step's `context_output` to the same file name so the prompt and gate agree.
- Produces db state (rows) or a side effect (a record created, a message sent, a lock updated) → a `db` rule: a read-only SQL assertion that the rows/values actually exist in `db/db.sqlite`. Do **not** declare a `context_output` — the runtime will not force a throwaway output file, and the step is checked on the real effect instead of a self-written marker.
- The file **is** the deliverable but the agent writes it → require run-specific **proof** inside it (real ids, values read back from the authoritative system, timestamps the real system produced), so the gate confirms real work, not a self-asserted "done".

A `db` rule is the stronger gate: it checks the source of truth the next step and the report actually read, so a fabricated or stale "success" can't pass by writing a tidy file. A step with **no** `validation_schema` has no gate at all — it is accepted on the agent's word (the weakest possible proof). Give any step whose result matters a real schema, db-first.

**Prevalidation is now the exception, not the default.** The step-level `validation_schema` above is already enforced automatically as the final gate with same-conversation repair — that covers nearly every case on its own. Presume a sequence needs no `prevalidation` item at all. Add one only when you can name a later item in the SAME sequence that must not run unless an intermediate artifact already passed — guarding something costly or hard to undo (a paid tool call, a fan-out, an external side effect), not routine double-checking. If the final configured item already validates the same step-level schema, the runtime does not add a duplicate final gate, and a `prevalidation` item that just restates that schema is pure waste: it burns a repair turn on every run instead of catching anything a later item couldn't have caught by simply failing the final gate.

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

For a step that persists to the db (the common case for state/side-effect work), gate on the db instead of a file — no `context_output` needed:

```json
{
  "id": "verify-persisted",
  "type": "prevalidation",
  "validation_schema": {
    "db": [
      {"name": "rows written this run", "sql": "SELECT status FROM processed WHERE run_id = 'iteration-0'", "min_rows": 1}
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

Conversational route sub-agents use `message_sequence`, including stateless one-turn work. Use `regular` only for an explicitly scripted deterministic route. Use these patterns when designing or repairing orchestrator predefined routes.

For an orchestrator route, use `message_sequence` when the orchestrator should preserve specialist memory. As an orchestrator predefined route, a message_sequence behaves like a reusable specialist sub-agent: Normal repeated calls reuse the route conversation and each call is delivered as a re-entry user message. Set `message_sequence_restart=true` to restart only when the prior conversation is stale, wrong, or contaminated.

## MESSAGE SEQUENCE ROUTE PATTERNS

- Stateful specialist: re-enter one route for follow-up work.
- Test/fix loop: validate externally, then re-enter the specialist with concrete failures.
- Maker/reviewer: keep creation and independent review in separate routes.
- Panel: separate domain specialists coordinated by an orchestrator.
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
