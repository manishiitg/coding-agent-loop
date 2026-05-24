## ROUTING STEP DESIGN

A routing step makes an LLM-based decision and branches to exactly one of N existing steps in the plan. Use it when the next step depends on a signal that requires judgment to evaluate — not on a deterministic check.

### When to use routing

- The path forward is **conditional on prior output or state** (e.g., "is the user logged in?", "what document type was uploaded?", "did the scrape return data or a CAPTCHA?")
- There are **2–N mutually exclusive paths** and only one should run
- The decision needs **LLM judgment**, not a deterministic check (for deterministic file/value checks, prefer validation_schema + retry, not routing)

### Two modes

- **Pure routing**: omit `description` on the routing step. The router only reads prior context and chooses a route. Use this when an earlier step already produced the signal.
- **Execute-then-route**: provide a `description` on the routing step. The router first performs a small task (e.g., classify, probe), then routes on its own output. Use this when the routing signal does not yet exist.

### Route structure

A routing step has:

- `routing_question` — the specific signal the router evaluates (required)
- `routes[]` — minimum 2 entries (required)
- `default_route_id` — optional fallback `route_id` used when the LLM picks an invalid route

Each entry in `routes[]` has:

- `route_id` / `route_name` — stable identifier the router selects
- `condition` — short prose describing when this route applies; the router matches `routing_question` against these
- `next_step_id` — the **ID of an existing step in the plan** that this route branches to

Routing routes do **not** define inline sub-agents. Unlike todo_task `predefined_routes` (which embed a `sub_agent_step`), routing `routes[]` are pointers — every `next_step_id` must reference a step that already exists in the plan. Add those downstream steps separately (as regular, message_sequence, todo_task, or human_input steps), then point the routes at their IDs.

### Routing vs. other primitives

- **Routing vs. todo_task**: todo_task runs **all** known sub-tasks. Routing runs **exactly one** of N alternatives. If "every form field" → todo_task; if "income form OR business form OR neither" → routing.
- **Routing vs. message_sequence**: message_sequence is one ordered conversation with no branching. If the path is linear, use message_sequence; if it forks, use routing.
- **Routing after human_input**: pair `human_input` → `routing` when the user's answer determines the path. The human_input step collects the answer; the routing step reads it and picks the route.

### Example

After a login step, a routing step with `routing_question` = "Did login succeed, hit MFA, or fail?" and three routes:

- `success` → `next_step_id: "step-extract-data"`
- `mfa_required` → `next_step_id: "step-mfa-prompt"` (a human_input step that asks for the code)
- `failed` → `next_step_id: "step-retry-or-abort"`

Each `next_step_id` must already exist as a step in the plan.

### Anti-patterns

- Routing with only one route, or with a route that handles "everything else" generically — collapse to a regular step.
- Using routing for deterministic checks (file exists, value > threshold) — use validation_schema or a regular step with conditional logic instead.
- Vague `routing_question` ("decide what to do next") — the question must name the specific signal being evaluated.
- `next_step_id` pointing to a step that does not exist yet — add the downstream steps first, then wire the routes.
- Pure routing (no `description`) when the routing signal does not exist in prior context — the router will always fall back to `default_route_id` or fail. Switch to execute-then-route, or add a prior step that produces the signal.
