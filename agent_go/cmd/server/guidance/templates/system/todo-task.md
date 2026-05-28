## todo_task — Orchestrator / Sub-Workflow / Pipeline Step

`todo_task` is the multi-task orchestration step type. Users call it
"orchestrator," "sub-workflow," or "pipeline," and the things inside it
"sub-agents." The internal type name is `todo_task`. Load this skill when
designing a new todo_task step, adding/restructuring routes, deciding
between inline `sub_agent_step` and shared `orphan_step_ref`, or
debugging route behavior.

For the broader plan-design framing (when to pick todo_task vs routing
vs message_sequence vs regular), the `plan-design` skill is the parent
reference.

## When to use todo_task

A todo_task step is right when the step **manages multiple discrete
tasks**, especially:

- The set of tasks is dynamic — discovered at runtime — and each must be
  executed
- Progress tracking matters — the UI shows which tasks are done,
  pending, failed, with retry counts
- Tasks may need different sub-agents per route
- One step should iterate over a list / dataset / set of items

**Don't use todo_task when:**

- The flow is a single linear conversation — use `message_sequence`
- The next step depends on a binary or N-way decision — use `routing`
- It's a single focused task with one tool/output — use `regular`
  (agentic) or `scripted`
- The orchestrator description grows into detailed instructions for ONE
  specific task — that task should be its own sub-agent route instead

## Anatomy

A todo_task plan step has two big parts:

```jsonc
{
  "id": "extract-bank-statements",
  "type": "todo_task",
  "description": "...high-level orchestration intent...",
  "todo_task_step": {
    // The orchestrator's own LLM-driven step — picks routes, tracks
    // progress, decides retries. Has its own description, validation,
    // and learnings.
  },
  "predefined_routes": [
    {
      "route_id": "process-each-account",
      "condition": "...when this route fires...",
      // EITHER an inline sub-agent step:
      "sub_agent_step": {
        "id": "process-account-inline",
        "type": "regular",  // or message_sequence, or todo_task (nested, 1 level only)
        "description": "...what this sub-agent does..."
      }
      // OR a reference to a plan-local orphan step (see below):
      // "orphan_step_ref": "shared-account-processor"
    }
  ]
}
```

**Two ways to define a route's worker — pick one per route, not both:**

- **Inline `sub_agent_step`**: a route-specific agent defined inside the
  route. Use when the work is tightly coupled to this orchestrator and
  not reused elsewhere.
- **`orphan_step_ref`** pointing to a plan-local orphan: use when the
  same sub-agent serves multiple orchestrators in this plan. The orphan
  step must declare `shared_with.orchestrator_ids` listing each
  orchestrator allowed to reuse it. The route then sets
  `orphan_step_ref: "<orphan-step-id>"`.

## Route sub-agent step types

A route's `sub_agent_step` can be:

- **`regular`** (the common case) — stateless one-off work per task.
- **`message_sequence`** — a stateful specialist conversation. Use when
  the sub-agent needs to remember prior turns across the orchestrator's
  invocations of this route (e.g., a reviewer that builds up critique
  context across iterations).
- **`todo_task`** (nested) — one nested orchestration layer for a route
  whose work itself decomposes into multiple sub-tasks.

**Nested todo_task limit**: only ONE nested layer is allowed.
top-level → nested-todo_task is valid; nested-todo_task containing
another nested todo_task is rejected. Break deeper hierarchies into
sibling orphan steps or message_sequence specialists.

## Variables and group_name

`run_full_workflow(group_name, ...)` and `execute_step(step_id, group_name, ...)`
both require explicit `group_name` because todo_task orchestrators
typically iterate over the variables in that group. The orchestrator
sees `$VAR_GROUP_NAME` and any per-group variables as env. When you
add a todo_task step, write the description so it explicitly reads
the group's variables / inputs rather than guessing.

## Anti-patterns

- **Inline sub-tasks in the orchestrator description**: if the
  `todo_task_step.description` contains specific instructions for a
  single sub-task (e.g., "for each account, parse the PDF, extract
  totals, then write to db"), those sub-tasks should be routes with
  their own sub-agent steps. The orchestrator's description should be
  about *coordination*, not *execution*.
- **One-route orchestrators**: a todo_task with only one route and no
  branching is over-engineered. Make it a `regular` step instead — the
  orchestrator shell adds no value.
- **Routing inside todo_task description**: if the orchestrator picks
  between mutually exclusive paths based on a single decision, use a
  `routing` step at that point, not narrative branching in the
  description.
- **Nested orphan_step_ref**: an orphan step can be referenced by
  multiple orchestrators only when its `shared_with.orchestrator_ids`
  explicitly lists each one. Don't assume reuse is automatic.

## Scripted-mode todo_task (fast-path orchestrator)

When the orchestrator's routing decisions are **stable and
deterministic** — the set of route calls is known in advance and only
branches on success/failure — you can author a
`learnings/{step-id}/main.py` and mark the step
`declared_execution_mode="scripted"` (only after the user explicitly
asks and 10+ scenario-covering successful runs prove the route behavior
is stable).

At runtime the script runs first; any failure falls back to the normal
LLM orchestrator with a fresh start. The orchestrator scripted path is
**read-only at runtime** — the builder writes main.py once at design
time, the runtime only runs it. There is no fix loop and no save-back.
Script failures surface so you can regenerate `main.py` manually.

For the full scripted-orchestrator authoring rules, call
`get_reference_doc(kind="optimize-playbook")` and read the
"Orchestrator scripted mode" section.

## Tools

- `add_todo_task_step(step_id, description, todo_task_step, ...)` — add
  a new todo_task to the plan.
- `update_todo_task_step(step_id, ...)` — update orchestrator metadata.
- `add_todo_task_route(step_id, route_id, condition, sub_agent_step | orphan_step_ref)` — add a route.
- `update_todo_task_route(step_id, route_id, ...)` — update a route.
- `delete_todo_task_route(step_id, route_id)` — remove a route.

When inspecting a todo_task step, prefer
`jq '.steps[] | select(.id == "<step-id>") | {type, todo_task_step, predefined_routes}' planning/plan.json`
over `cat planning/plan.json | less`.

## Designing well

1. Write the **orchestrator's description** about coordination —
   discovering tasks, choosing routes, retrying, finishing. Not about
   the work each task does.
2. Identify **2–4 routes** that cover the expected branches. More than
   ~5 routes is a sign the orchestrator is doing too much; consider
   splitting.
3. For each route, decide: inline `sub_agent_step` (specific, not
   reusable) or `orphan_step_ref` (shared, reusable).
4. If a route's work is multi-step + dynamic, consider making it a
   nested `todo_task` — but only one nested layer.
5. **Validation** lives on the orchestrator's `todo_task_step` (whether
   the overall set of tasks completed successfully) and on each
   sub-agent step (whether that task's specific output is valid).
