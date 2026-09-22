**Plan-editing tool arguments:** Before a plan mutation, read
`builder-reference/references/plan-editing-tools.md`. Step fields belong inside
`add_step.step` or `update_step.changes`; route fields belong inside
`manage_step_route.parameters`. Reading this reference grants no tools.

## Orchestrator — Legacy Compatibility for a Routed Agent

The canonical adaptive agent is now a `message_sequence` with optional
`predefined_routes`. Users may still call it an orchestrator, sub-workflow, or
pipeline, and older plans may persist `orchestrator` or `todo_task`. Those legacy
types remain readable for compatibility, but new plans should use the unified
agent shape.

At runtime both shapes use the same message-sequence executor, common system
prompt, default tool policy, persistent conversation, validation/repair loop,
and closing turns. Routes add only the specialist catalog, delegation guidance,
sub-agent tools, and asynchronous child lifecycle.

## Prompt boundary

- `description` is the durable system-level Step Charter: objective, boundaries,
  evidence authority, and definition of done.
- `items[]` are ordered user messages describing what to do on each turn.
- `predefined_routes` are capabilities the agent may call, skip, repeat, or
  re-enter; they are not an execution checklist.
- Live Workshop `human_input` and `call_sub_agent` instructions are user
  messages. Never append them to the saved description.
- `validation_schema`, workspace/store rules, and tool policy are system
  contract. Passing final validation is completion; starting or completing a
  child is not.

Do not copy the description into the first item. A first item should say what
the agent should do now under the charter, such as “Investigate the evidence,
choose any useful specialists, and produce the supported conclusion.”

## Canonical shape

```jsonc
{
  "id": "investigate-performance",
  "type": "message_sequence",
  "title": "Investigate performance",
  "description": "Determine the supported cause of the performance regression, preserve conflicting evidence, and persist a conclusion that passes the declared validation contract.",
  "items": [
    {
      "id": "investigate",
      "type": "user_message",
      "message": "Inspect current evidence, call useful specialists when their isolation or expertise helps, reconcile their results, and produce the conclusion."
    },
    {
      "id": "verify",
      "type": "user_message",
      "message": "Re-open authoritative evidence, challenge every material claim, repair unsupported conclusions, and recheck the result."
    }
  ],
  "predefined_routes": [
    {
      "route_id": "source-checker",
      "condition": "Use when a material claim needs independent source verification.",
      "sub_agent_step": {
        "id": "source-checker",
        "type": "message_sequence",
        "description": "Verify assigned claims against authoritative sources and report discrepancies with evidence.",
        "items": [
          {
            "id": "check",
            "type": "user_message",
            "message": "Check the assigned claims and return evidence-backed discrepancies."
          }
        ]
      }
    }
  ]
}
```

## When specialist routes are justified

Add routes only when the parent agent makes a real runtime orchestration
decision that a static plan cannot directly express:

- evidence determines which investigation is useful next;
- competing hypotheses require different specialists;
- a result changes the strategy or recovery approach;
- a specialist needs clean context, separate permissions, reusable learnings,
  independent validation, or re-entry with memory.

A fixed child set and order does not justify adaptive routes. Known deterministic
work belongs in scripted steps or `scripted` items. Known isolated agentic work
may remain explicit message-sequence plan steps. Parallelism, progress display,
and waiting for every result are supporting properties, not eligibility.

Available routes can be known upfront. Unknown work breakdown is one use case,
not a prerequisite: the agent may choose among known specialists based on live
evidence. Use scope already supplied by the user, variables, or upstream output;
ask only for information that is actually missing.

## Route workers

A route uses exactly one worker definition:

- inline `sub_agent_step` for route-specific work; or
- `orphan_step_ref` for a plan-local reusable worker whose `shared_with` contract
  permits this parent.

Worker types:

- `message_sequence` for conversational specialist work. Repeated calls in the
  same run resume that route conversation; set `message_sequence_restart=true`
  only when a clean restart is intentional.
- `regular` for saved deterministic code invoked through declared
  `script_parameters`.
- a `message_sequence` with its own routes for one nested delegation layer.

Only one nested delegation layer is allowed. Do not create a routed specialist
that recursively owns another routed specialist.

Pass a saved specialist only dynamic facts it cannot obtain from its own
description, dependencies, schema, skills, and learnings. A generic agent has no
saved contract, so its instruction must be self-contained.

## Child lifecycle

Sub-agent calls return an execution ID, not a result. The parent may launch
independent children together and then ends its turn. The runtime waits outside
the model call and sends an authoritative completion batch into the same
conversation. Do not poll normal completion, and never report completion while
a child remains pending. Inspect failed children before retrying or changing
strategy.

The parent owns reasoning, evidence reconciliation, final validation, and the
result. A dispatcher that only starts workers and concatenates their answers is
not a sound routed agent.

## Legacy migration

For an existing `orchestrator` / `todo_task` record:

1. Preserve its description as the new message-sequence system charter.
2. Convert legacy `messages` directly to `items`; do not synthesize a user turn
   from the description.
3. Preserve `predefined_routes`, route worker contracts, orphan references,
   validation, permissions, learnings, models, and next-step wiring.
4. Move any per-run instructions previously appended to description into an
   initial user item or runtime delegation message.
5. Validate the changed plan and inspect downstream contracts before treating
   migration as complete.

Use `maintain_plan(action="migrate_orchestrator_types", ...)` only when that
action is exposed and its contract matches the desired migration. Do not hand
rewrite compatibility fields without inspecting the live schema.

## Authoring tools

- `add_step(type="message_sequence", step={...})` creates the canonical agent.
- `update_step(step_id, changes={...})` edits its charter, items, validation, or
  other native fields.
- `manage_step_route(action, parameters={...})` adds, updates, or deletes a
  specialist route.
- `validate_plan_change(...)` checks the resulting plan contract.

Legacy tool aliases and `todo_task_step` may appear in old artifacts and event
names, but they are not the authoring model for new work.

## Anti-patterns

- Description repeated as item 0.
- Live delegation instructions appended to the system charter.
- Routes treated as a mandatory fixed checklist.
- Parent reports success because children returned, before final validation.
- Generic agent used to rewrite a validated specialist’s declared output.
- Dynamic runtime strategy encoded as deterministic `routing`; use routing only
  for an exclusive fixed branch selected by an existing decision source.
- Deeper than one nested delegation layer.
