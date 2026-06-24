## MESSAGE SEQUENCE — THE DEFAULT STEP TYPE

`message_sequence` is the **first-class default** for agentic work: one shared-context
conversation that does the work and then **verifies it in follow-up user-message turns** —
"do A, then check/critique it against the success criteria, then fix and write the final
output." Modern agents do a lot in a single long-running turn, so this beats the old habit of
splitting a task into several smaller `regular` steps that hand off context through
intermediate `.md`/`.json` files.

**Prefer one larger `message_sequence` with verification turns over many small regular steps.**
Where you used to chain `extract → validate → transform → check → write` as five regular steps
passing files between them, author one sequence: the work happens in shared context, and
**verification user-messages confirm each part is done and complete** before the final output —
no lossy file handoffs, no fresh step reconstructing what the last one was thinking.

Use it when (i.e. the common case):
- The turns mostly read the same upstream context.
- The later turns need the earlier turns' transient reasoning, critique, tool output, or accumulated conversation state.
- There is one final durable output, or tightly coupled outputs that can be validated together.
- Intermediate artifacts are only scratch/context handoff and are not useful to downstream workflow steps.
- The whole unit should fail/retry together.

Do not use it when:
- Each phase has an independent durable artifact, validation gate, retry/failure domain, or downstream consumer.
- Different phases require different tools, credentials, security isolation, or persistent-store contracts.
- The workflow needs deterministic branching; use `routing`.
- The workflow needs independent sub-agent delegation or progress over many tasks; use `todo_task`.

## ITEM ACCESS — reads are open, writes are per-item (declare them!)

Every item in a sequence can **read** `db/`, the knowledgebase, learnings, and soul — always, no
config. But **writes are off by default, scoped per item** (least-privilege, so a read/verify
turn can't mutate shared state). **Any item that writes must declare it** — this is the single
most common message-sequence mistake, so set it up front:

- writes a db table (`upsert into trade_ideas`, inserts/updates `db/db.sqlite`) → `"write_access": {"db": true}` (or `"kind": "db"`)
- writes a knowledgebase note → `"write_access": {"knowledgebase": true}` (or `"kind": "knowledgebase"`)
- writes durable learnings → `"write_access": {"learnings": true}` (or `"kind": "learning"`)
- a `code` item is auto-granted from its `output_files` paths (an `output_files` under `db/` enables db writes)

An undeclared write is **not** silently dropped — the runtime folder guard blocks it and the item
fails with `STATUS: FAILED — grant write_access.db on the item`. Grant it in the plan so the write
lands the first time. Reads never need a grant.

## MESSAGE SEQUENCE ROUTE PATTERNS

Use these patterns when designing or hardening todo_task predefined routes:

- **Stateful Specialist**: one todo_task route owns an expert conversation the orchestrator can re-enter across feedback loops.
- **Test/Fix Loop**: orchestrator calls the same message_sequence route, runs validation/tests, then re-enters it with failures instead of starting over.
- **Maker + Reviewer**: one route creates output and a separate message_sequence reviewer route remembers standards, prior issues, and review history.
- **Panel of Specialists**: several message_sequence routes keep separate memory for different domains while the todo_task orchestrator coordinates decisions.
- **Clean-Room Retry**: use `message_sequence_restart=true` when a clean second attempt is needed because prior context is stale, wrong, or contaminated.
- **Human-in-the-Loop Re-entry**: human_input or operator feedback can be sent back into the same sequence conversation as the next user message.
- **Top-Level Scripted Conversation**: use a top-level message_sequence when the workflow is a fixed linear conversation and does not need orchestration.

For a todo_task route, use `message_sequence` when the orchestrator should preserve specialist memory across critique, test feedback, validation feedback, or follow-up calls. Reuse the same route for re-entry; restart only when the prior conversation is stale, wrong, or contaminated.

Route sub-agents can be `regular` for stateless one-off work or `message_sequence` for a stateful specialist conversation (a route may also be a nested `todo_task`). For ad-hoc broad-access work the orchestrator can `call_generic_agent` at runtime — that is a delegation tool, not a route sub-agent type, so do not author a route with `type: "generic_agent"`. Normal repeated calls reuse the route conversation — the orchestrator sends the next re-entry user message to the same conversation. Set `message_sequence_restart=true` only when starting fresh is required (stale, contaminated, or wrong-direction conversations). As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent.

## HOW MEMORY WORKS (route vs standalone)

The two roles differ only in whether the conversation is remembered:

- **Route** (a `message_sequence` inside a todo_task `predefined_routes`): the orchestrator re-enters the same specialist across its calls and the conversation **is remembered** — but that memory is **in-memory, scoped to a single workflow run**. It is *not* written to disk for resume, does *not* survive a process restart, and does *not* carry into a later run. `message_sequence_restart=true` clears it (and wipes the route's runtime artifacts) for a clean start. This is the only place re-entry memory exists.
- **Standalone** (a top-level `message_sequence` step): a **fixed item queue that runs once**. No memory, no re-entry — re-running simply re-runs the queue. This is the right shape for the single-step quality patterns below.
- `session.json` is written in both cases as a one-way **observability log** of what the conversation said; it is never read back to resume.

So: need a specialist that remembers across the orchestrator's own repeated calls → **route**. Need a one-shot quality gate on a unit of work → **standalone**. Need to "rerun at a later time / resume across runs" → that does not exist here; drive it from the orchestrator (route) or a scheduler.

## DESCRIPTION IS TURN 0 (consistent across every step type)

A step's `description` is **the opening instruction and IS executed** — it leads the first user turn of the conversation. This is one uniform rule across the workflow, so author `description` as actionable, never as throwaway metadata:

- **todo_task orchestrator**: `description` is rendered directly as the orchestrator's first user turn (the task framing).
- **standalone `message_sequence`**: `description` leads turn 0 — it is prepended to `items[0]` in the same conversation; `items[]` are turns 1..N.
- **route `message_sequence`**: the route's `description` plus the orchestrator's per-call `call_sub_agent` instructions form the opening instruction prepended to `items[0]` on the first call.

Practical consequences for plan design:
- Put the real opening instruction in `description`; put the follow-up turns in `items[]`. Don't leave `description` as a vague label and stuff the actual first instruction into `items[0]` — they will both run, back-to-back, in turn 0.
- A `message_sequence` with a substantive `description` should have at least one `user_message`/`foreach` item to carry it as a turn. Description-only with no conversational item does no work.

## SINGLE-STEP QUALITY PATTERNS

The patterns above use message_sequence as a todo_task route (a reusable specialist the orchestrator re-enters). message_sequence is equally useful as a **standalone step** that keeps same-context ordered turns together and makes one unit of work trustworthy, using the item queue (`user_message` + `code` + `prevalidation`, all sharing one conversation):

- **Self-Validation Gate**: after a work turn, add `user_message` items that interrogate the same conversation about what it actually did — "Did you actually call `xyz`? Quote the exact output. Did you actually produce `abc`?" — then a `prevalidation` item whose schema checks the concrete artifacts. Interleave several interrogate→prevalidation pairs, each prevalidation using a different schema, to gate distinct claims before the step completes.
- **Compute-then-Reason**: alternate `code` items (fetch/parse/compute ground truth) with `user_message` items that reason over the result. The runtime feeds each code item's stdout + exit code into the next message turn, so the agent reasons over real computed data instead of its own assumptions.
- **Citation / Grounding Gate**: a `user_message` that forces the agent to cite the exact file/line/tool-output behind each claim, then a `prevalidation` that the cited files exist. The enforced way to catch hallucinated claims.
- **Self-Healing Script**: on a `code` item set `on_failure: repair_with_llm` with `max_retries` (and `save_repaired_script` to persist the fix). The same conversation debugs its own failing script across attempts before the step fails.

Briefer variants: **Plan-then-Execute** (turn 1 plans, optional prevalidation of the plan, turn 2 executes — planning and doing share one memory); **Dry-Run-then-Commit** (a `user_message` states the exact side effects, then a `code` item performs them under item-scoped `write_access`, then `prevalidation` confirms the result); **Accumulator** (each turn appends one piece to a kb/db artifact; conversation memory prevents duplication).

**Constraint:** the item queue is linear and runs once — no branching, no conditional skip, and no "loop until prevalidation passes" inside a single sequence (a failed prevalidation hard-stops the step after configured code-repair attempts). Iteration comes only from orchestrator re-entry / `message_sequence_restart` (the route patterns above) or the code-item repair loop. For retry-until-green, use the **Test/Fix Loop** route pattern, not a standalone sequence.

## IN-BETWEEN PREVALIDATION AND LEARNINGS

Yes for **prevalidation**: add an item like `{ "type": "prevalidation", "validation_schema": {...} }` anywhere in `items[]`. It runs against the message_sequence step execution folder (`runs/<iteration>/execution/<step-id>/`) and blocks the next item if the required files/fields are not present. Prefer several small gates after the item that should have produced each artifact instead of one giant final schema.

Learning is different: there is no automatic "learning phase" between sequence items. The normal learning system is step-level and runs after the whole step when `learnings_access="read-write"` plus `learning_objective` are configured on the step. If you need a deliberate in-sequence learning write, use a `user_message` item with `kind: "learning"` or `write_access: {"learnings": true}` and tell it exactly what durable HOW guidance to update under `learnings/_global/`. Keep this rare and explicit; do not use it for observations that belong in KB or db.

Example:

```jsonc
{
  "id": "draft",
  "type": "user_message",
  "message": "Draft the outreach copy and write output/draft.json."
},
{
  "id": "draft-schema",
  "type": "prevalidation",
  "validation_schema": {
    "files": [
      { "file_name": "output/draft.json", "must_exist": true,
        "json_checks": [{ "path": "$.subject", "must_exist": true, "value_type": "string" }] }
    ]
  }
},
{
  "id": "capture-how",
  "type": "user_message",
  "kind": "learning",
  "message": "If this run exposed a reusable HOW pattern for drafting subject lines, update learnings/_global/SKILL.md with the minimal durable rule. If there is no durable pattern, say no learning update is needed."
}
```

## DATA-DRIVEN ITERATION (`foreach`)

Items are usually fixed at design time. A **`foreach`** item instead generates turns at **runtime** from `db/db.sqlite` table rows — a reliable for-loop. Use it for the producer/consumer pattern: an earlier step writes rows to a table, and this step processes **every** row. `source_sql` is a read-only query; each result row binds to `.` in the message template.

```jsonc
{ "type": "foreach",
  "source_sql": "SELECT id, desc FROM tasks WHERE status='pending'",  // read-only query against db/db.sqlite
  "message": "Process task {{"{{"}}.id{{"}}"}}: {{"{{"}}.desc{{"}}"}}. Upsert the result into the results table: sqlite3 db/db.sqlite \"INSERT INTO results(id, ...) VALUES(...) ON CONFLICT(id) DO UPDATE SET ...\".",
  "max_iterations": 0             // optional cap; 0 = all rows (capping is logged, never silent)
}
```

- The runtime reads the array and sends **one `user_message` turn per row**, with the row bound to `.` in a Go `text/template` (`{{"{{"}}.field{{"}}"}}`). All turns share the same conversation (auto-summarization keeps context bounded).
- **Why it's reliable:** the loop is in code, not the LLM — every row gets its own turn, so nothing is skipped. This is the right tool when a prior step produced a list the LLM must work through exhaustively.
- `foreach` mixes with static items: e.g. a static intro turn, the `foreach` loop, then a static summary/`prevalidation` turn.
- `foreach` has no per-row prevalidation; gate after the loop with a static `prevalidation` item if needed.
- Available the same way on a **todo_task** step's `messages` (one orchestrator turn per row, so the orchestrator can delegate to sub-agents per row).
