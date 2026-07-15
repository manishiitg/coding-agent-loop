## Planning Steps

When a user describes what to automate: take action. Create or update
the plan using workflow-builder best practices and the available
context. Ask the user only for blocking choices that would materially
change behavior, safety, credentials, schedules, external side effects,
or irreversible actions. State reasonable assumptions briefly and
proceed.

Every plan-tool mutation is an atomic graph transaction. Before saving,
the backend verifies that every routing route and explicit
`next_step_id`/human-input branch targets a step that still exists or
`end`. If a tool returns `PLAN_GRAPH_INVALID`, no partial edit was saved:
use the listed source step and field to reroute every dangling reference,
then retry the original add/update/delete. Never delete a referenced step
first and plan to repair the graph afterward.

## Step composition defaults

**Default to `message_sequence`.** Modern agents do a lot in a single
turn and run for a long time, so the strongest shape is one
shared-context conversation that does the work and then **verifies it in
follow-up items** — e.g. `[do the task] → [verify the result against the
success criteria] → [fix what verification caught]`. Building the
verification in as later messages of the same sequence keeps the agent's
full working context instead of forcing a fresh `regular` step to
reconstruct it from artifacts. Every step still needs a
`validation_schema`; context flow stays forward-only via
`context_dependencies` → `context_output`.

Use `regular` only when the step is genuinely **one atomic action with no
verify-and-fix follow-up**, or when a turn needs its own **durable
artifact, hard validation gate, retry/failure domain, different
tool/security context, or a distinct downstream consumer** — those are
the real reasons to split into separate steps.

For fixed branch choices the user already gave the builder, prefer a
deterministic `routing` step and pass `route_selections` when running.
Use `todo_task` when the step manages multiple discrete tasks or needs
sub-agent delegation. Do not add a `human_input` step just to ask the
same branch choice again.

## Step types

- **`message_sequence`** (default) — ordered turns sharing one
  conversation: do the work, then verify/fix in follow-up user messages
- **`regular`** — single atomic action with no verify-and-fix follow-up
- **`todo_task`** — orchestrator with `predefined_routes`; each route
  is `message_sequence` / `regular` / nested `todo_task` (1 level
  deep only)
- **`routing`** — choose next step from a fixed route map
- **`human_input`** — pause for operator input
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
