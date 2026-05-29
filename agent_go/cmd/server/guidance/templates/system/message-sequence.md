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

Route sub-agents can be `regular` for stateless one-off work, `message_sequence` for a stateful specialist conversation, or `generic_agent` for an agent with broad workspace access. Normal repeated calls reuse the route conversation — the orchestrator sends the next re-entry user message to the same conversation. Set `message_sequence_restart=true` only when starting fresh is required (stale, contaminated, or wrong-direction conversations). As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent.

## HOW MEMORY WORKS (route vs standalone)

The two roles differ only in whether the conversation is remembered:

- **Route** (a `message_sequence` inside a todo_task `predefined_routes`): the orchestrator re-enters the same specialist across its calls and the conversation **is remembered** — but that memory is **in-memory, scoped to a single workflow run**. It is *not* written to disk for resume, does *not* survive a process restart, and does *not* carry into a later run. `message_sequence_restart=true` clears it (and wipes the route's runtime artifacts) for a clean start. This is the only place re-entry memory exists.
- **Standalone** (a top-level `message_sequence` step): a **fixed item queue that runs once**. No memory, no re-entry — re-running simply re-runs the queue. This is the right shape for the single-step quality patterns below.
- `session.json` is written in both cases as a one-way **observability log** of what the conversation said; it is never read back to resume.

So: need a specialist that remembers across the orchestrator's own repeated calls → **route**. Need a one-shot quality gate on a unit of work → **standalone**. Need to "rerun at a later time / resume across runs" → that does not exist here; drive it from the orchestrator (route) or a scheduler.

## SINGLE-STEP QUALITY PATTERNS

The patterns above use message_sequence as a todo_task route (a reusable specialist the orchestrator re-enters). message_sequence is equally useful as a **standalone step** that makes one unit of work trustworthy, using the item queue (`user_message` + `code` + `prevalidation`, all sharing one conversation):

- **Self-Validation Gate**: after a work turn, add `user_message` items that interrogate the same conversation about what it actually did — "Did you actually call `xyz`? Quote the exact output. Did you actually produce `abc`?" — then a `prevalidation` item whose schema checks the concrete artifacts. Interleave several interrogate→prevalidation pairs, each prevalidation using a different schema, to gate distinct claims before the step completes.
- **Compute-then-Reason**: alternate `code` items (fetch/parse/compute ground truth) with `user_message` items that reason over the result. The runtime feeds each code item's stdout + exit code into the next message turn, so the agent reasons over real computed data instead of its own assumptions.
- **Citation / Grounding Gate**: a `user_message` that forces the agent to cite the exact file/line/tool-output behind each claim, then a `prevalidation` that the cited files exist. The enforced way to catch hallucinated claims.
- **Self-Healing Script**: on a `code` item set `on_failure: repair_with_llm` with `max_retries` (and `save_repaired_script` to persist the fix). The same conversation debugs its own failing script across attempts before the step fails.

Briefer variants: **Plan-then-Execute** (turn 1 plans, optional prevalidation of the plan, turn 2 executes — planning and doing share one memory); **Dry-Run-then-Commit** (a `user_message` states the exact side effects, then a `code` item performs them under item-scoped `write_access`, then `prevalidation` confirms the result); **Accumulator** (each turn appends one piece to a kb/db artifact; conversation memory prevents duplication).

**Constraint:** the item queue is linear and runs once — no branching, no conditional skip, and no "loop until prevalidation passes" inside a single sequence (a failed prevalidation hard-stops the step after configured code-repair attempts). Iteration comes only from orchestrator re-entry / `message_sequence_restart` (the route patterns above) or the code-item repair loop. For retry-until-green, use the **Test/Fix Loop** route pattern, not a standalone sequence.

## DATA-DRIVEN ITERATION (`foreach`)

Items are usually fixed at design time. A **`foreach`** item instead generates turns at **runtime** from a db file — a reliable for-loop. Use it for the producer/consumer pattern: an earlier step writes rows to `db/<file>.json`, and this step processes **every** row.

```jsonc
{ "type": "foreach",
  "source": "db/tasks.json",      // workspace-relative JSON array (or use source_path for a nested array)
  "source_path": "items",         // optional dot-path to the array field
  "message": "Process task {{"{{"}}.id{{"}}"}}: {{"{{"}}.desc{{"}}"}}. Write the result to db/results.json keyed by {{"{{"}}.id{{"}}"}}.",
  "max_iterations": 0             // optional cap; 0 = all rows (capping is logged, never silent)
}
```

- The runtime reads the array and sends **one `user_message` turn per row**, with the row bound to `.` in a Go `text/template` (`{{"{{"}}.field{{"}}"}}`). All turns share the same conversation (auto-summarization keeps context bounded).
- **Why it's reliable:** the loop is in code, not the LLM — every row gets its own turn, so nothing is skipped. This is the right tool when a prior step produced a list the LLM must work through exhaustively.
- `foreach` mixes with static items: e.g. a static intro turn, the `foreach` loop, then a static summary/`prevalidation` turn.
- `foreach` has no per-row prevalidation; gate after the loop with a static `prevalidation` item if needed.
- Available the same way on a **todo_task** step's `messages` (one orchestrator turn per row, so the orchestrator can delegate to sub-agents per row).
