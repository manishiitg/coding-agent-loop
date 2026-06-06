## ROUTING STEP DESIGN

A routing step is a deterministic switch. It reads `route_selection.json`, resolves the selected value to one of its `routes[]`, and branches to that route's `next_step_id`.

Use routing when the workflow must run **exactly one** of N existing downstream steps. Do not put judgment inside the routing step itself; put judgment in an earlier regular step, human_input step, caller-provided `route_selections`, or execute-then-route probe that writes the route file.

### When to use routing

- The path forward is conditional on a known signal (e.g., "logged in", "MFA required", "document type is invoice")
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

### Two modes

- **Pure routing**: omit `description`. The routing step reads its own `route_selection.json`, `route_source_file`, or a `context_dependencies` entry named `route_selection.json`. Use this when a prior step already produced the route file or the caller will pass `route_selections`.
- **Execute-then-route**: provide `description`. The step first performs a small deterministic/probe task and writes `route_selection.json` into its own output folder, then the router reads it.

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
- **Routing after human_input**: pair `human_input` -> `routing` when the user's answer determines the path. Either pass `route_selections` from the caller or add a small step that maps the answer to `route_selection.json`.

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
- `next_step_id` pointing to a step that does not exist yet.
- Pure routing with no caller `route_selections`, no `route_source_file`, no `route_selection.json` dependency, and no `default_route_id`.
