## Planning Steps

When a user describes what to automate: take action. Create or update
the plan using workflow-builder best practices and the available
context. Ask the user only for blocking choices that would materially
change behavior, safety, credentials, schedules, external side effects,
or irreversible actions. State reasonable assumptions briefly and
proceed.

## Step composition defaults

Default to `regular` unless the task clearly needs branching,
iteration, sub-agents, or same-context ordered turns that should share
one conversation. Every step must have a `validation_schema`. Context
flow is forward-only via `context_dependencies` → `context_output`.

For fixed branch choices the user already gave the builder, prefer a
deterministic `routing` step and pass `route_selections` when running.
Do not add a `human_input` step just to ask the same branch choice
again.

If multiple ordered agent actions share the same context and mainly
need to build on each other, prefer `message_sequence` over multiple
regular steps. Split into regular steps only when a durable output,
validation gate, retry/failure domain, different tool/security context,
or downstream consumer needs a separate artifact.

## Step types

- **`regular`** — single agentic step
- **`todo_task`** — orchestrator with `predefined_routes`; each route
  is `regular` / `message_sequence` / nested `todo_task` (1 level
  deep only)
- **`routing`** — choose next step from a fixed route map
- **`human_input`** — pause for operator input
- **`message_sequence`** — ordered turns sharing one conversation/context
- **orphan** (`is_orphan: true`) — reusable across orchestrators via
  `shared_with.orchestrator_ids` + `orphan_step_ref`

## See also

For the design playbook (8-step design walkthrough, step-type
trade-offs, validation design, context flow, anti-patterns,
step-types reference, inner steps, reusable orphan-route pattern),
call **`get_reference_doc(kind="plan-design")`** — this is the entry
point for any plan-composition decision. From there:

- **Per-step-type deep dives**: `todo-task` (anatomy + routes +
  nested + scripted fast-path), `human-input` (input types + routing
  pairing + unattended schedules), `message-sequence` (full pattern
  catalog: Stateful Specialist, Test/Fix Loop, Maker+Reviewer, Panel,
	  Clean-Room Retry, HITL Re-entry, Scripted Conversation), `routing`
	  (deterministic route_selection.json contract, anti-patterns).
- **Recurring multi-step shapes**: `workflow-patterns` (Phase Router,
  Scoped Investigation, Linear Pipeline, Fan-out & Consolidate,
  Verification Gate, Pre-flight Probe, Human Checkpoint, Critique
  Loop, Persistence Tail).
