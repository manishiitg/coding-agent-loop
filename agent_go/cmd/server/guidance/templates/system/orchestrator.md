## orchestrator — Orchestrator / Sub-Workflow / Pipeline Step

`orchestrator` is the multi-task orchestration step type. Users call it
"orchestrator," "sub-workflow," or "pipeline," and the things inside it
"sub-agents." The plan type is `orchestrator`; `todo_task` is the legacy
alias older plans still carry, which the runtime keeps reading (contract
v1.0.35 rewrites it via migrate_orchestrator_step_type). Load this skill when
designing a new orchestrator step, adding/restructuring routes, deciding
between inline `sub_agent_step` and shared `orphan_step_ref`, or
debugging route behavior.

At runtime an orchestrator **is a `message_sequence` that owns routes**: it
runs on the same executor (one conversation, ordered items, in-place
prevalidation repairs, a final validation gate, a closing reflection turn),
plus the sub-agent tools, an async child lifecycle, and a narrower folder
guard. Everything in the `message-sequence` reference about items, foreach,
prevalidation, and write access applies here unchanged.

For the broader plan-design framing (when to pick orchestrator vs routing
vs message_sequence vs regular), the `plan-design` skill is the authoritative
parent reference. This file explains how to author an already-justified
orchestrator; it does not relax that parent's eligibility rule.

## When to use orchestrator

An orchestrator step is right only when its parent makes a **real runtime
orchestration decision that the static plan cannot directly express**, such as:

- The set of tasks is dynamic — discovered at runtime — and each must be
  executed
- The parent reasons about runtime evidence to select further investigations or workers
- The parent interprets results and changes its strategy, including choosing a different recovery approach
- Interim synthesis reveals missing evidence and changes subsequent delegation

**A fixed child set and order does not justify an `orchestrator` step.** Different tools,
separate learnings, progress visibility, and easier debugging are supporting
properties after this eligibility gate, not sufficient reasons by themselves.

**Don't use orchestrator when:**

- The flow is a single linear conversation — use `message_sequence`
- Several known actions share one objective, context, and output/retry contract — keep them in one large `message_sequence`
- Several known independent fixed actions can be declared as plan steps with dependencies — do that instead of adding an LLM parent
- A list/dataset can be processed in one shared conversation — use a `foreach` item inside `message_sequence`
- The next step depends on a binary or N-way decision — use `routing`
- It's a single focused conversational task with one output — use
  `message_sequence`; use `regular` only for a deterministic script
- The orchestrator description grows into detailed instructions for ONE
  specific task — that task should be its own sub-agent route instead

The parent is the reasoning owner: it forms hypotheses, weighs evidence, decides
next work, and can synthesize or write the final report itself. Delegation supports
that work; starting a fixed list and waiting for results is not enough. A known
batch of scripts is sequence work by design. Sequence child support is scoped to
scripts only for now; separate agentic children remain an orchestrator capability.
Known isolated agentic tasks can be separate message-sequence plan steps without
an adaptive parent. See `references/plan-design.md`, Step 2, for scripted batch support and its limits.
Script batches do not grant agentic delegation to message sequences.

## Anatomy

An orchestrator plan step has two big parts:

```jsonc
{
  "id": "extract-bank-statements",
  "type": "orchestrator",
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
        "type": "regular",  // or message_sequence, or orchestrator (nested, 1 level only)
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

- **`message_sequence`** (the default for agentic route work) — one large
  shared-context specialist span with its own proof, double-check, repair, and
  validation. It can also remember prior turns across the orchestrator's
  invocations of this route.
- **`regular`** — an explicitly scripted deterministic route that needs no same-context
  proof/repair follow-up, or deterministic scripted route work.
- **`orchestrator`** (nested) — one nested orchestration layer for a route
  whose work itself decomposes into multiple sub-tasks.

**Nested orchestrator limit**: only ONE nested layer is allowed.
top-level → nested-orchestrator is valid; nested-orchestrator containing
another nested orchestrator is rejected. Break deeper hierarchies into
sibling orphan steps or message_sequence specialists.

For a `regular` route, inspect its declared `script_parameters` and invoke it with
`call_scripted_sub_agent(route_id, task_id, parameters={...})`. The controller applies
defaults and rejects missing, unknown, or wrongly typed values before `main.py` starts.
Do not pass free-form instructions to a parameterized script or ask it to rewrite itself;
scripts adapt only through their declared `STEP_PARAMS_JSON` values. Use `call_sub_agent`
for conversational routes.

## Routes vs generic agent vs self-execution

At runtime the orchestrator chooses *how* to do each unit of work — design with
that in mind:

- **Predefined route** — define one only when the work is a **reusable
  specialist that should learn and be validated** (routes carry learning +
  prevalidation + tiering and persist recipes across runs). This is the only
  delegation path that improves over time.
- **Background agent** (`run_in_background`) — the workshop uses it for
  **ad-hoc** work it wants offloaded: isolated context, parallelizable, cheaper
  tier — but **no** learning/prevalidation. Don't create a route for one-off,
  unspecialized work; leave it to the generic agent.
- **Self-execution** — the parent owns substantive analysis, strategy, decisions,
  and synthesis, and may perform bounded direct work using its available tools.
  It is not restricted to small glue tasks or forbidden from writing the report.

Rule of thumb: a route earns its place only when its work is a reusable
specialist (and you have ≥2 of them, or genuine coordination). One-off or
unspecialized work needs **no** route — see Anti-patterns.

**Context isolation is a tradeoff, not a free win.** Delegating keeps the
orchestrator's context lean and enables parallelism — but sub-agents (routes and
generic alike) can't see what the orchestrator knows, so it must re-pass all
relevant context in every call. Work that is tightly coupled to accumulated
context is often cheaper to self-execute than to delegate. Don't design a step so
finely sharded that the orchestrator spends more effort re-briefing sub-agents
than the isolation saves.

## Variables and group_name

`run_full_workflow(group_name, ...)` and `execute_step(step_id, group_name, ...)`
both require explicit `group_name` because orchestrator orchestrators
typically iterate over the variables in that group. The orchestrator
sees `$VAR_GROUP_NAME` and any per-group variables as env. When you
add an orchestrator step, write the description so it explicitly reads
the group's variables / inputs rather than guessing.

## Messages (long, multi-phase tasks)

An orchestrator step can carry an optional ordered `messages` list. These are
ordinary message-sequence items: after the opening turn (the step description),
each entry is fed into the **same orchestrator conversation** in order, so the
orchestrator works through the phases with full memory of prior turns and every
sub-agent result ("do phase 1" → "now phase 2 using what you found" → "now
reconcile and write the report").

- Item types: `user_message` (alias `message`), `prevalidation` (a hard gate
  between turns — on failure the orchestrator receives the failed checks as a
  repair turn in the same conversation and re-runs the gate, up to three
  repairs), and `foreach` (one orchestrator turn per `db/db.sqlite` row from a
  read-only `source_sql`, row bound to `.` in the `message` template — the
  deterministic producer/consumer loop; delegate per row as needed).
- Code and file items are rejected at plan validation and again at runtime:
  the orchestrator delegates that work to a sub-agent route.
- After every item the runtime waits for the children that item launched and
  feeds one completion batch back before the next item starts. A `foreach`
  that launches a child per row therefore blocks per row, not per step.
- The step-level `validation_schema` runs as a synthetic final gate after the
  last item, with the same in-place repair loop. There is no restart-the-step
  retry: repairs keep the conversation and its sub-agent results.
- It all runs in **one execution** — no persistence and no re-entry. For a
  specialist that resumes across the orchestrator's *own repeated calls*, use a
  `message_sequence` **route** instead (see the `message-sequence` reference).

## Anti-patterns

- **Dispatcher-only parent**: starting known workers, waiting, and assembling
  results does not justify an orchestrator. Use the sequence design and current
  capability guidance in `references/plan-design.md`.
- **Outsourcing all reasoning**: the parent must own strategy and synthesis.
  Extract bounded specialist instructions into routes where useful, but keep
  the parent's substantive reasoning contract in its description.
- **Choosing by route count**: one or many routes does not establish eligibility.
  An adaptive investigator may use one specialist repeatedly. A fixed ten-worker
  script batch is sequence work by design; known isolated agentic tasks can be
  separate plan steps. Deterministic work uses scripted regular steps.
- **Routing inside orchestrator description**: if the orchestrator picks
  between mutually exclusive paths based on a single decision, use a
  `routing` step at that point, not narrative branching in the
  description.
- **Nested orphan_step_ref**: an orphan step can be referenced by
  multiple orchestrators only when its `shared_with.orchestrator_ids`
  explicitly lists each one. Don't assume reuse is automatic.

## Tools

- `add_orchestrator_step(step_id, description, todo_task_step, ...)` — add
  a new orchestrator to the plan.
- `update_orchestrator_step(step_id, ...)` — update orchestrator metadata.
- `add_orchestrator_route(step_id, route_id, condition, sub_agent_step | orphan_step_ref)` — add a route.
- `update_orchestrator_route(step_id, route_id, ...)` — update a route.
- `delete_orchestrator_route(step_id, route_id)` — remove a route.

When inspecting an orchestrator step, prefer
`jq '.steps[] | select(.id == "<step-id>") | {type, todo_task_step, predefined_routes}' planning/plan.json`
over `cat planning/plan.json | less`.

## Designing well

1. Write the **orchestrator's description** around the reasoning it owns:
   evidence to interpret, decisions to make, when to change direction, and what
   proves completion. It may perform analysis and final synthesis directly.
2. Identify bounded specialist routes that support that reasoning. Choose the
   route count from the work, not a numerical quota; a fixed batch alone does
   not justify an orchestrator.
3. For each route, decide: inline `sub_agent_step` (specific, not
   reusable) or `orphan_step_ref` (shared, reusable).
4. If a route's work is multi-step + dynamic, consider making it a
   nested `orchestrator` — but only one nested layer.
5. **Validation** lives on the orchestrator's `todo_task_step` (whether
   the overall set of tasks completed successfully) and on each
   sub-agent step (whether that task's specific output is valid).
