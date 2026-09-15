# Orchestrator Step Type

## Overview

A `todo_task` step is an **orchestrator**: a conversational agent that owns a
set of predefined routes (sub-agents) and decides at runtime what to delegate,
what to do itself, and how to react to what comes back. Users call it
"orchestrator", "sub-workflow", or "pipeline". The plan type is `orchestrator` since contract v1.0.35; `todo_task` is the legacy
alias the runtime still reads (the todo list it was named for no longer exists).

Since PLAT-269 the orchestrator **runs on the message_sequence executor**. It
is a message sequence that owns routes: one conversation, ordered items, an
in-place prevalidation repair loop, a synthetic final validation gate, and a
closing reflection turn — plus the sub-agent tools, an asynchronous child
lifecycle, and a narrower folder guard.

**When to use it.** Only when the parent makes a real runtime orchestration
decision the static plan cannot express: a dynamic task set discovered at
runtime, conditional selection or fan-out per item, material runtime parallel
coordination, adaptive retry, an approval boundary, or an interim synthesis
that changes later delegation. A fixed child set and order does not justify a
`todo_task`; that is plain plan steps, a `message_sequence`, a `routing`
step, or a scripted step (see the `todo-task` and `plan-design` guidance).

## Plan JSON shape

```json
{
  "type": "todo_task",
  "id": "step-grow",
  "title": "Grow the account",
  "description": "Opening instruction — executed as the orchestrator's first turn.",
  "context_dependencies": ["execution/step-research/research.json"],
  "context_output": "growth-report.md",
  "validation_schema": { "files": [ { "path": "growth-report.md", "must_exist": true } ] },
  "predefined_routes": [
    {
      "route_id": "draft-post",
      "route_name": "Draft a post",
      "condition": "When a post needs to be written",
      "sub_agent_step": { "type": "message_sequence", "id": "draft-post", "title": "...", "description": "...", "items": [ ... ] }
    },
    { "route_id": "shared-reviewer", "route_name": "Reviewer", "orphan_step_ref": "reviewer" }
  ],
  "messages": [
    { "id": "phase-2", "type": "user_message", "message": "Now reconcile the drafts and write the report." },
    { "id": "gate", "type": "prevalidation", "validation_schema": { "files": [ ... ] } }
  ],
  "next_step_id": "step-publish"
}
```

- `predefined_routes[].sub_agent_step` may be `message_sequence` (the default
  for any conversational specialist; repeated calls resume its conversation),
  `regular` (a deterministic scripted boundary), or `todo_task` (one nested
  orchestrator level, no deeper).
- `messages` are ordinary message-sequence items: `user_message` (alias
  `message`), `prevalidation`, `foreach`. Code and file items are rejected at
  plan validation and again at runtime.
- Step config (`planning/step_config.json`) is the same as any step:
  `execution_llm`, tools, skills, store access. `declared_execution_mode:
  scripted` is rejected for a `todo_task`.

## Runtime

`executeOrchestratorStep` (`controller_orchestrator.go`) is a thin wrapper:

1. Computes the orchestrator folder guard: the run's execution folder plus
   `db/`, learnings and KB when granted, skills read-only. It deliberately
   does **not** grant the workflow root, so a nested orchestrator cannot read
   `workflow.json`, `variables/`, `planning/`, or sibling groups.
2. Builds the orchestrator prompt variables (`buildOrchestratorTemplateVars`):
   the route catalog, inlined context dependencies, stores, folder guard,
   validation schema, and any Workshop human input.
3. Builds the `SubAgentExecutionContext` that owns async children.
4. Constructs a synthetic `MessageSequencePlanStep` whose items are one
   opening `user_message` (the step description) followed by `messages`, and
   calls `executeMessageSequenceStep` with a `Delegation`.
5. Emits `todo_task_step_completed` and returns `next_step_id`.

The `messageSequenceDelegation` seam (`controller_message_sequence.go`)
carries: the exec context, the folder guard paths, an agent factory (the
orchestrator agent type with sub-agent tools — the progress bridge from
PLAT-027 keys on that type), the per-turn template variables, and the
`todo_task` execution-log writer.

With a delegation set, the executor:

- creates the orchestrator agent once and reuses it for every item;
- after every conversational turn waits for the children that turn launched
  and feeds one `[AUTO-NOTIFICATION] SUB-AGENT COMPLETION BATCH` back into the
  same conversation (`reconcileAsyncSubAgentCalls`, up to 64 rounds) before
  the next item — on the error path too; a `foreach` that launches one child
  per row therefore blocks per row;
- runs `prevalidation` items and the synthetic final gate with the in-place
  repair loop (three repair turns); an exhausted gate fails the step with an
  error, it is not marked completed;
- appends the closing reflection turn (learnings / knowledgebase) from step
  config, exactly as a standalone sequence does;
- guarantees `cancelOutstandingAndWait` on every exit path;
- does not take the global learnings mutex on the reflection turn (a parent
  waiting for children inside the turn would deadlock against a child's own
  learnings turn).

There is no scripted fast path and no restart-the-whole-step retry.

### Async sub-agent lifecycle

`call_sub_agent` and `call_generic_agent` register the call, spawn a
goroutine under a cancellable child context, and return an `execution_id`
immediately. Several calls in one turn run concurrently. The wait happens
outside the LLM call: the executor blocks on every unreconciled child, then
delivers one batch sorted by start time, failures included as terminal
results. `query_sub_agent` and `stop_sub_agent` act on the exact
`execution_id`; `get_sub_agent_conversation` pages one execution's transcript.
See `sub_agent_async.go`.

### Tool arguments

- `route_id` — parent-side selector for a configured route.
- `task_id` — a stable label for one unit of delegated work, used for tracking
  and artifact naming (`execution/<step>/<route>-<task_id>/`). Formerly
  `todo_id`; it never referred to a persisted todo item.
- `execution_id` — runtime identity of one exact invocation; the only identity
  used for query, stop, and conversation diagnostics.
- `preferred_tier` — required 1/2/3 reasoning tier per call.
- `message_sequence_restart` — for message_sequence routes only; archive the
  route conversation and replay its configured queue.

## Artifacts and logs

- Orchestrator turns: `execution-attempt-1-iteration-<n>.json` plus
  `-conversation.json` and `-timing.json` under the step's log folder, written
  by `saveOrchestratorExecutionLog`. Turn 1 is iteration 0. These carry the
  `SubAgentCallRecord`s that `ExecutionLogsPopup` renders as the
  route-selection view and populate `stepLogs.todo_task`.
- Sequence session: `execution/<step>/session.json` (one-way observability
  log, not a resume checkpoint) and per-gate prevalidation logs.
- Sub-agent artifacts: `execution/<step>/<route>-<task_id>/`.
- Prompts: `todo-task-prompts.json` pre-saved so `get_step_prompts` works
  during execution.

## Events

`todo_task_route_selected` (per delegation, from the tool wrapper),
`todo_task_step_completed` (after children settle), plus the standard step
started/finished and pre-validation events. The progress bridge keeps the
`todo_task_orchestrator` turn open until `todo_task_step_completed` arrives
(PLAT-027). The old `todo_task_item_*` events were never emitted and have
been removed.

## LLM selection

`selectOrchestratorLLM`: step-config `execution_llm` wins; otherwise
tier 1 (high) from the tier resolver. Sub-agents inherit the step's
`execution_llm` when set, else honor `preferred_tier`.

## Frontend

The workflow graph renders a `todo_task` as a compound node with its routes
as children (`usePlanToFlow.ts`). `ExecutionLogsPopup.tsx` reads the
`stepLogs.todo_task` category and the per-call decision records. Neither
needed changes for the executor merge because the log shape and event names
were kept.

## History

- PLAT-027: async completion no longer falsely completes the parent.
- PLAT-082: failed async children reported correctly.
- PLAT-151: dead `context_to_pass` removed from routes.
- PLAT-322: merged identifier and workflow-browser inheritance boundary
  (`route_id` / `task_id` / `execution_id`).
- PLAT-259: `branch` split from `routing`; route `sub_agent_step` types.
- PLAT-269: scripted orchestrator path removed, dead todo list deleted,
  `todo_id` → `task_id`, orchestrator moved onto the message_sequence executor.
