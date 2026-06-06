## Planning Steps

When a user describes what to automate: **present the plan and get
explicit confirmation before creating any steps.** The user may be
exploring — do not assume they are ready to commit.

## Step composition defaults

Default to `regular` unless the task clearly needs branching,
iteration, or sub-agents. Every step must have a `validation_schema`.
Context flow is forward-only via `context_dependencies` →
`context_output`.

## Step types

- **`regular`** — single agentic step
- **`todo_task`** — orchestrator with `predefined_routes`; each route
  is `regular` / `message_sequence` / nested `todo_task` (1 level
  deep only)
- **`routing`** — choose next step from a fixed route map
- **`human_input`** — pause for operator input
- **`message_sequence`** — multi-message conversation pattern
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
