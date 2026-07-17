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

**Default to one large `message_sequence` per shared-context span.** Modern
agents can own many actions and long tool sessions, so the strongest shape is
one conversation that completes the coherent span and then proves it in
follow-up items: `[do the whole span] → [re-open evidence and prove every
criterion] → [repair gaps and double-check]`. Improve the description,
run-specific proof/provenance output, top-level `validation_schema`, and
verify/repair turns before adding another step. A request for more checking is
not itself a step boundary.

Use multiple large sequences when their contexts should not be shared: distinct
credentials/security exposure, independent durable outputs or retries,
clean-room independence, human/routing boundaries, or unrelated context that
would distract or contaminate the next agent. The builder must decide this from
the workflow semantics and be able to state the boundary. Use `regular` for a
coherent one-turn outcome with no same-context repair loop, or for deterministic
work that must be scripted. Every producing step still needs a
`validation_schema`; context flow stays forward-only via
`context_dependencies` → `context_output`.

For fixed branch choices the user already gave the builder, prefer a
deterministic `routing` step and pass `route_selections` when running.
Use `todo_task` only when independent sub-agent delegation itself adds value;
several known actions in one shared context are not enough. Do not add a
`human_input` step just to ask the same branch choice again.

## Step types

- **`message_sequence`** (default for reasoning) — one substantial outcome
  sharing one conversation: process persisted evidence, then verify/fix in
  focused follow-up user messages
- **`regular`** — one coherent output and final gate. Deterministic API/SDK/CLI
  fetching, parsing, normalization, and mechanical persistence should be
  scripted regular steps; batch related calls rather than making micro-steps
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
