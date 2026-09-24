[← Pulse platform issue index](../../pulse_platform_issue_register.md)

# PLAT-357 — Crew functions: typed calls between Crews and workflows

| Coordination | Value |
|---|---|
| Assigned agent | Claude Code |
| Ticket state | `vertical slice on main; not deployed; live RTS check pending` |
| Last synchronized | `2026-09-24` |
| Priority | `P1 product` |
| Category | integrations (runner-up: scheduler-runs) |

## Problem

Crews and workflows can already call each other through secretless internal
triggers (`connect_to_target` / `call_target` / `get_target_run` /
`send_to_target_run`, f7130b4f8). A trigger call is a free-form task string
and the answer is whatever prose the target ends with. The caller cannot see
what a target offers, cannot rely on the shape of the answer, cannot get a
quick answer inside its own turn, and cannot ask a long call how it is going
without interrupting it.

## Design

**Functions.** A Crew or workflow declares named functions in
`<target>/functions.json`:

```json
{"version":1,"functions":[{
  "name":"run_login_flow",
  "description":"Run the RTS login flow against a build",
  "input_schema":{"type":"object","required":["build"],"properties":{
     "build":{"type":"string"},"env":{"type":"string","enum":["staging","prod"]}}},
  "result_schema":{"type":"object","required":["passed"],"properties":{
     "passed":{"type":"boolean"},"failed_step":{"type":"integer"},"trace_path":{"type":"string"}}},
  "instructions":"Run tests/login_flow.py with BUILD=<build> ...",
  "created_by":"crew:alpha (Alpha Bot)", "created_at":"...", "updated_at":"..."}]}
```

Schemas are a JSON Schema subset: `object` (properties, required),
`array` (items), `string`, `number`, `integer`, `boolean`, `enum`. A
function is managed like a trigger (`define_function`, `delete_function`,
`list_functions`); any Crew may define functions on any Crew (Crews are
shared read-write server-wide); workflows need owner/editor access.
Creation records the creator.

**Calling.** `call_function(target, function, args)` validates `args`
against the input schema, then dispatches through the target's standard
internal trigger binding (the same durable run and turn queue as
`call_target`: idle → now, busy → after the current turn). A Crew target
receives the function's instructions, the validated arguments, a `call_id`
and a result contract: it must end by calling
`return_function_result(call_id, result)`, which is validated against the
result schema (an invalid result is rejected with the validation error so
the target can fix it; after two rejections the call fails). A Crew run
that ends without a result gets one retry turn, then the call fails with
its final text as the reason. A workflow target runs its plan with the
arguments in the payload; its result is the run's outcome (step outputs),
not schema-validated in this slice.

- **Fast path:** if the call completes within 120 s, `call_function`
  returns the result directly as its tool result.
- **Slow path:** it returns `{status:"running", call_id}` and, unless
  `notify=false`, the caller's chat is resumed with an `[AUTO-NOTIFICATION]`
  carrying the result, failure or timeout (the same background-execution
  pipeline as `trigger_and_auto_notify` / `call_target`).

**Generated tools.** For Crews and workflows tagged (`#crew:` /
`#workflow:`) or attached to the calling Crew, each function is also
registered as its own tool, e.g. `rts_flow_tester__run_login_flow(build,
env)`, with its input schema as parameters. Any other function stays
reachable through `list_functions` + `call_function`.

**Progress and updates mid-run.**

- `report_function_progress(call_id, message, percent?)` — the target
  reports milestones (it is told to in the call instructions); the latest
  10 entries are kept on the call record.
- `get_function_call(call_id)` — status, result/error, latest progress,
  and a short tail of the target's recent activity (last assistant text,
  current tool) read from its session events, without interrupting it.
  `get_target_run` (plain trigger calls) returns the same activity tail.
- `ask_function_update(call_id, question)` — Crew targets only: delivers a
  question into the target's running turn through the existing
  `send_to_target_run` live-input path; the target answers with
  `report_function_progress`, which the caller sees on its next poll.

**Guards.** No self-calls. A call chain is propagated through in-flight
calls: a target already in the chain is refused (cycle), depth is capped at
4, and one chain may make at most 20 calls. `return_function_result` and
`report_function_progress` are accepted only from the call's own target.

## Not in this slice

- Call records live in memory (with a JSON copy under
  `<target>/functions/calls/`); the auto-notification watch does not
  survive a server restart (same as `call_target`), `get_function_call`
  falls back to the saved copy.
- No UI yet for functions; they appear in `functions.json` and through the
  tools. Workflow results are not schema-validated.
