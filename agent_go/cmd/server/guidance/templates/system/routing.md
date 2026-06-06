## ROUTING STEP DESIGN

A routing step is a deterministic switch. It reads `route_selection.json`, resolves the selected value to one of its `routes[]`, and branches to that route's `next_step_id`.

Use routing when the workflow must run **exactly one** of N existing downstream steps. The common case is a fixed branch selected from the user's request to the builder; the builder/caller passes that choice as `route_selections` when starting the workflow. Do not put judgment inside the routing step itself; put judgment in an earlier regular step or caller-provided `route_selections`. If an agent decision is needed, add a regular step before routing that writes `route_selection.json` in its own output folder.

### When to use routing

- The path forward is conditional on a known signal (e.g., "logged in", "MFA required", "document type is invoice")
- The user already told the builder which fixed workflow mode/job/branch to run
- There are 2-N mutually exclusive paths and only one should run
- The selected path can be represented as a stable `route_id` or a unique `next_step_id`

### Route selection contract

The router reads this file shape:

```json
{
  "select_route": "route_id_here"
}
```

Compatibility aliases are accepted: `route_id` and `selected_route_id`.

The value may be:

- a `routes[].route_id`
- a unique `routes[].next_step_id`

If the file exists but is invalid, routing fails. If no file exists, `default_route_id` is used when set; otherwise routing fails. Routing never silently chooses the first route.

### Single mode

Routing steps never execute agents. Leave `description` and `context_output` empty. The step reads a caller-preseeded `route_selection.json`, an explicit `route_source_file`, a `context_dependencies` entry named `route_selection.json`, or `default_route_id`.

When an agent/probe/classifier must decide the route, model it as:

- prior `regular` step: performs the probe/classification and writes `route_selection.json`
- routing step: declares `route_source_file` or `context_dependencies: ["route_selection.json"]` and branches from that file

### Route structure

A routing step has:

- `routing_question` — retained for plan readability and compatibility
- `routes[]` — minimum 2 entries (required)
- `default_route_id` — optional fallback `route_id` used when no route file exists
- `route_source_file` — optional explicit route file source produced by a prior step

Each entry in `routes[]` has:

- `route_id` / `route_name` — stable identifier the route file selects
- `condition` — short prose explaining when this route should be selected
- `next_step_id` — the ID of an existing step in the plan that this route branches to

Routing routes do **not** define inline sub-agents. Unlike todo_task `predefined_routes` (which embed a `sub_agent_step`), routing `routes[]` are pointers — every `next_step_id` must reference a step that already exists in the plan. Add those downstream steps separately (as regular, message_sequence, todo_task, or human_input steps), then point the routes at their IDs.

### Routing vs. other primitives

- **Routing vs. todo_task**: todo_task can run multiple sub-tasks. Routing runs exactly one alternative.
- **Routing vs. message_sequence**: message_sequence is one ordered conversation with no branching.
- **Routing vs. human_input**: do not ask the user again when the builder already knows the requested branch. Use `route_selections`. Use `human_input` only when the workflow must pause mid-run for information that was not available at launch.

### Example

After a login probe, write:

```json
{
  "select_route": "mfa_required"
}
```

Then route among:

- `success` -> `next_step_id: "step-extract-data"`
- `mfa_required` -> `next_step_id: "step-mfa-prompt"` (a human_input step that asks for the code)
- `failed` -> `next_step_id: "step-retry-or-abort"`

Each `next_step_id` must already exist as a step in the plan.

### Anti-patterns

- Routing with only one route, or with a generic catch-all route that should be normal step logic.
- Asking the routing step to infer the route from prose without writing `route_selection.json`.
- Setting `description` on a routing step. Use a prior regular step for probe/judgment work.
- `next_step_id` pointing to a step that does not exist yet.
- Routing with no caller `route_selections`, no `route_source_file`, no `route_selection.json` dependency, and no `default_route_id`.
