# Calling Crews and workflows

Status: implementation in progress, 2026-09-25. Direct MCP calling, input
defaults, refusals, and the Run now form are implemented. Generated gateway
targets and function-backed webhook, schedule, and Slack bindings remain later
phases. Builds on [crew-calls.md](../crew-calls.md),
which describes the existing function calls, caller conversations, timeouts and
late answers.

## Decision

Give Crews and workflows one **caller-facing contract**: discover a target,
`ask` it in plain language, call a named function with checked inputs, and
follow the resulting call. A Crew still works in a caller conversation; a
workflow function still starts a route run. The shared contract does not erase
those execution differences.

Keep webhooks and schedules in this design as a **later phase**. First make
direct calls useful and safe. Then let each webhook or schedule bind to a
function while retaining its own authentication, timing, delivery and retry
rules. Existing standalone triggers keep working.

Workflow `ask` may choose an exposed function **or** a raw Run-mode action.
It should prefer a matching function and ask for missing values, but that is
assistant behavior, not the typed-call guarantee. Only `call_function` and
bindings to a function guarantee input validation before execution. A
function's `allowed_callers` restricts that entry point; it is not a security
boundary around the underlying workflow route. A route that must be restricted
needs a route or Run-mode policy as well.

## Why

- MCP currently has `get_crew` / `ask_crew` for Crews, but `get_workflow`,
  `call_workflow_function(function: "ask")`, and the older `chat` / `run_status`
  path for workflows. The connection instructions present workflow functions
  as typed runs, so clients can miss workflow `ask` entirely.
- Function inputs such as owner and repo must be repeated on every direct
  call. The Functions UI has no Run button.
- An invalid call returns an error without enough schema detail to correct it.
- Webhooks, schedules and functions can select the same route but are managed
  as separate entry points. Bringing them together is useful after direct
  calling is consistent.

## Terms and boundaries

| Term | Meaning |
|---|---|
| Agent | A Crew or workflow that can be addressed by stable ID or unambiguous name. |
| Function | A named, typed entry point. A Crew function runs a task and validates its result; a workflow function chooses a route and supplies run variables. |
| `ask` | A conversational entry point. Crew `ask` returns the Crew's reply; workflow `ask` reaches its Run-mode assistant. |
| Caller | The authenticated principal starting a call: a Crew, workflow, person in the UI, MCP connection, or (later) a webhook or schedule binding. |
| Call | A durable record for one `ask` turn or function invocation, identified by `call_id`. |
| Binding | A webhook or schedule configuration that invokes a function after its own trigger conditions are met. |

One caller has one continuing `ask` conversation with each target. Crew typed
calls from that caller use the same Crew conversation. Workflow typed calls
start separate runs; they do not become turns in its `ask` conversation.
`chat` remains available through the gateway for callers that need several
parallel workflow conversations.

## Identity and permissions

Authorization is checked on the server at discovery, call creation and read
back. A `target` name or ID in request JSON never determines caller identity.

- A Crew or workflow caller uses its existing stable project/manifest ID.
- A person pressing **Run now** uses their user ID.
- A new MCP call uses the access token's stable, non-secret **connection ID**
  and its owning user ID. Token name is display text, not identity. Calls and
  conversations are isolated by connection ID, so two tokens belonging to one
  user do not share a target conversation or call results. Revoked tokens
  cannot start or poll calls. The target owner can still see call history.
- Existing `call_tool` aliases and legacy CLI calls retain their current
  user-scoped caller identity and conversations for compatibility. The new
  direct tools use the connection identity. The call record says which
  identity model created it, so polling never guesses.
- Later, a webhook or schedule binding uses its own binding ID as caller.
  The authenticated delivery or scheduler supplies that ID; payload fields
  cannot impersonate another caller.

Base permission is required before a function allow-list is considered:

| Caller path | Base permission |
|---|---|
| MCP → Crew | `crews:run` and token access to that Crew. |
| MCP → workflow | `runs:execute`, token access to that workflow, and owner/editor workflow access. |
| UI → Crew | Owner/editor run access to that Crew. |
| UI → workflow | Owner/editor workflow access. |
| Crew/workflow → target | Existing internal caller authorization. |
| Webhook/schedule → function | An enabled binding owned by the target, plus webhook authentication where applicable. |

`allowed_callers` is an optional list of stable caller principals on a typed
function. An empty list permits every caller with base permission. Extend the
Crew function definition and the workflow function caller stamp to represent
people and connection IDs; validate the list on save and enforce it in the
common dispatcher. The built-in `ask` uses base permission; if a Crew declares
its own `ask`, that declaration takes precedence and its allow-list applies.
The workflow assistant sees only functions it may call,
and dispatch still checks permissions. A user who can `ask` a workflow may be
able to request the same route as a raw run, subject to Run-mode policy.

Discovery is scoped before name matching. Unknown and out-of-scope targets
produce the same `not_found` response. Ambiguous visible names return only
the visible matches and their IDs. Call-result access is limited to the
initiating principal and the target's owner/editor view; another connection
of the same user cannot poll it. A read-only token cannot start calls.

## Direct calling

### MCP surface

The remote MCP endpoint currently advertises two tools: `get_api_spec` and
`call_tool`. It will advertise **six fixed tools**:

| Advertised MCP tool | Purpose |
|---|---|
| `list_agents` | Find visible Crews and workflows, their callable function signatures and coarse availability. |
| `ask` | Start one free-text turn in the caller's continuing conversation. |
| `call_function` | Start a typed call after server-side input validation. |
| `get_call` | Read the progress and result of either kind of call. |
| `get_api_spec` | Discover less common operations and their exact schemas. |
| `call_tool` | Execute an operation discovered through `get_api_spec`. |

Scope-filter tool advertisement and `get_api_spec` results. A token with read
access but no run permission sees `list_agents`, `get_api_spec` and `call_tool`,
but not `ask` or `call_function`. `get_call` is visible when the token can
read calls it made; it still checks the individual call record. The REST
external API and the MCP tools share one implementation for authorization,
validation, dispatch and response formatting.

The connection instructions say: discover with `list_agents`; use `ask` for
plain-language requests; use `call_function` when the function and inputs are
known; follow a returned `call_id` with `get_call`; use `get_api_spec` for
other operations. Tool descriptions carry this guidance because some hosted
clients do not show MCP initialize instructions.

### `list_agents(query?, kind?, limit?, offset?)`

Return a bounded, paginated list of visible targets. An exact ID selects one
target. Name matching for later calls ignores case and punctuation; ambiguous
matches are refused rather than selecting a preferred kind. IDs remain the
recommended durable reference.

```json
{
  "agents": [
    {
      "id": "wf_fc1adcb0",
      "name": "RTS PR Reviewer",
      "kind": "workflow",
      "about": "Reviews pull requests and reports findings.",
      "functions": ["review_pr(PR_NUMBER: integer, GROUP?: staging|prod)"],
      "busy": true
    }
  ],
  "total": 1,
  "has_more": false
}
```

Signatures show every caller-supplied input, including optional inputs and
inputs that have defaults. Mark a default with `=…` only when its value is
safe and short to display; otherwise mark it `=default`. This lets callers
discover an override without provoking an invalid call. Include a `can` value
(`"call"` or `"read"`) only when useful to distinguish a visible target's
permissions. Show only functions this principal may invoke. `busy` is a
boolean, not another caller's task text. Never include paths, prompts, plans,
files or current inputs in discovery.

### `ask(target, message, wait_seconds?)`

Resolve the target and check base run permission. Empty messages are refused
before a call is created. The server continues the initiating principal's
one conversation with that target; it queues a second turn behind an active
turn in that same conversation. Different principals have separate
conversations. `wait_seconds` defaults to 20 and is capped at 25 to stay under
the hosted proxy timeout.

The workflow Run-mode assistant may answer, call an allowed typed function,
or start a raw run. Its prompt lists callable functions, input descriptions
and defaults, asks for missing important values, and favors a matching
function. It does not promise that every conversational request went through
the typed validator. The result reports what path was taken (`function`,
`raw_run` or `answer_only`) when known. Crew `ask` returns its final reply.

`ask` has no `about` or `new_conversation` parameter. A general mid-run
message cannot be delivered to every kind of call: existing
`ask_function_update` reaches Crew turns but workflow route runs reject it.
Keep that Crew-specific operation behind `call_tool`. `chat` remains the
explicit way to create parallel workflow conversations.

### `call_function(target, function, args, wait_seconds?)`

Resolve the target and function, check base and function permissions, then
apply defaults and validate all inputs **before dispatch**. A typed call
either starts the named function exactly as requested or is refused. It never
falls back to `ask` or a saved workflow variable. An omitted `args` object is
treated as `{}`.

Rules for the shared validator:

- Omitted inputs receive a declared default; an explicit value wins.
  Explicit `null` is not omission and is rejected unless the schema allows it.
- Required-with-default inputs may be omitted. Validate a default against
  type, enum and other supported constraints when saving the function.
- Accept only documented lossless conversions, such as a decimal integer
  string to an integer and the exact strings `"true"` / `"false"` to booleans.
  Apply the same conversions to defaults and caller input.
- Reject unknown arguments. An unknown value cannot silently fall back to a
  default. Report all independent input problems in one response.
- For workflows with multiple allowed groups, include the existing `group`
  choice in the effective schema and signature; validate it before dispatch.
- Do not manufacture executable examples. A refusal can return the supplied
  valid values and declared defaults, but missing values remain missing.

Example refusal:

```json
{
  "status": "refused",
  "code": "invalid_inputs",
  "problems": ["missing PR_NUMBER", "GROUP must be staging or prod"],
  "function": {
    "name": "review_pr",
    "inputs": [
      {"name": "PR_NUMBER", "type": "integer", "required": true},
      {"name": "GITHUB_OWNER", "type": "string", "default": "runloop-works"},
      {"name": "GROUP", "type": "string", "enum": ["staging", "prod"]}
    ]
  },
  "retry_with": {"target": "wf_fc1adcb0", "function": "review_pr", "args": {"GITHUB_OWNER": "runloop-works"}},
  "missing": ["PR_NUMBER"]
}
```

The client must supply `missing` values before retrying. An unknown function
returns the target's callable signatures. A forbidden function returns no
schema details beyond what that principal could already discover.

### Call lifecycle and `get_call(call_id, wait_seconds?)`

Both start tools return `{status: "completed", call_id, result}` if the turn
finishes within the wait, or `{status: "working", call_id, progress, next}`.
An `ask` result also includes `answer`; a workflow function result also
includes its run reference. A pre-dispatch refusal returns `status:
"refused"` and no `call_id`.

`get_call` waits up to 25 seconds for a state or progress change. It returns
one normalized `status`: `queued`, `working`, `completed`, `failed` or
`interrupted`. Separate `timed_out` and `late` booleans preserve the existing
behavior: an idle timeout releases the caller while target work may continue;
a later answer updates the same call. `interrupted` means the server restarted
before the call settled. The response includes the last five timestamped
progress reports, the last activity time, and any permitted short activity
summary. An error can include `partial_result` and the target's final reply.
It never represents a timed-out call as a completed cancellation.

The call record is durable and indexed by ID. New long polling needs a
progress-change signal or bounded watcher in addition to today's completion
channel. On restart, a still-open record becomes `interrupted` and remains
readable; the dispatcher must not silently retry a possibly executed action.

There is **no universal cancel operation** in this phase. Sending `"stop"`
through `ask` or a Crew update request is only a message, not a cancellation
guarantee. Workflow execution stop operations remain available through the
gateway to callers authorized for those specific executions. The UI should
show a Stop action only where an actual stop API exists.

### Generated function names

In-platform Crew chats retain their existing generated tools. MCP
`get_api_spec` may also list direct per-function targets for callers that
want a single `call_tool` operation. They are **gateway targets**, not extra
advertised MCP tools. Build this list at request time from the connection's
visible and permitted functions; the current external catalog is cached and
cannot hold per-user dynamic names.

Do not silently discard name collisions. A generated key includes the stable
agent ID and function name. A readable name-based alias may be shown only if
unique in that connection's catalog. `call_tool` must still perform the same
permission and input checks as `call_function`. This convenience follows the
four core operations; it is not needed for the first release.

## Function cards and defaults

Automation → Functions shows a card for each Crew and workflow function,
with the built-in `ask` first. A card shows its description, effective input
schema, route/task summary, callers and recent calls. For workflow functions,
the card may link to the actual route and run. The UI does not imply that
Crew and workflow functions execute identically.

| Card action | Behavior |
|---|---|
| **Run now** | Build a form from the supported schema. Show defaults, required fields and enum choices. Validate client-side for feedback and server-side before dispatch. Show progress and result inline. |
| **Defaults** | Save typed values on the function definition, with validation at save time. |
| **Callers** | Show the function allow-list and each caller conversation or run history, subject to permissions. |
| **Copy call** | Copy an MCP `call_function` request or curl request with placeholders for required values. Show a Slack example only when a Slack binding exists. |
| **Bindings** | In the later trigger phase, manage schedules and webhooks that invoke this function. |

**Run now** calls the same dispatcher as MCP with the viewer's user ID, then
shows the call status and a link to its conversation or run. A reader can see
the card but not start it. The server checks both base and function
permissions even if the client displays a Run button.

`WorkflowFunctionInput` gains an optional typed `default`. Crew function
`input_schema` uses a validated `default` on each property. The effective
schema used by discovery, the form, refusal and dispatch comes from one
server-side builder. Defaults are applied to the invocation; they do not
mutate the saved workflow variable set. The UI may remember a viewer's last
form values in browser storage, but no server call depends on that state.
The form distinguishes a prefilled last value from a function default.

## Later phase: webhooks and schedules as function callers

A function remains the typed route/task definition. A webhook or schedule is
an independent **binding** that starts it. One function may have several
bindings. A binding stores `function_id`, its own enabled state and caller
identity, plus fields specific to delivery:

| Binding | Owns |
|---|---|
| Schedule | Cron/calendar, timezone, fixed inputs, retry/collision policy and run history. |
| Webhook | URL, bearer or provider signature secret, payload-to-input mapping, replay/idempotency record, retry response and delivery history. |
| Slack | Channel/app routing and identity checks; this is a separate adapter after webhook and schedule bindings. |

The bound function owns the route selection, allowed groups and input
contract. On delivery, the binding authenticates its source, constructs only
declared function arguments, applies defaults, validates, and dispatches using
the binding ID as caller. Extra raw webhook payload fields remain available
as evidence but are not passed as function arguments. A mapping to an unknown
input is rejected when saving the binding; an absent or mistyped runtime
value produces a failed delivery with no function run. Re-check fixed
schedule inputs when the function changes and before every fire. Show invalid
bindings on the card so they can be fixed; never execute them with stale
variables. Preserve raw payload, signature verification, idempotency and
existing delivery status semantics.

Crew schedules already deliver a prompt. A Crew **function schedule** instead
calls the named function with fixed inputs in that schedule binding's own
conversation. Existing prompt schedules remain available. `allowed_callers`
may name a binding ID; the binding itself also needs to be enabled and owned
by the target.

### Migration

Existing webhooks and schedules remain standalone and continue using their
saved route, allowed variables, timing and authentication. Show them in their
current views with **Turn into a function**. That action previews the new
function and binding, including input mappings, defaults, group choice and
any behavior that cannot be represented. It writes both records atomically
and preserves the existing webhook URL/secret or schedule timing and history
identity. If the conversion cannot preserve behavior, refuse it with an
explanation; do not partially migrate. Nothing converts automatically.

Standalone triggers can coexist with function bindings for as long as
existing users need them. The new card is the preferred place for new
function-backed bindings, without claiming that every trigger is a function.

## Compatibility and rollout

`get_api_spec` / `call_tool` keep the old remote operation names and their
responses: `list_crews`, `list_workflows`, `get_crew`, `get_workflow`,
`ask_crew`, `call_crew_function`, `call_workflow_function`, their pollers and
the other existing catalog operations. The `agentworks` CLI keeps its current
`crews` and `functions` commands for scripts and continues to call these
names. Do not remove an alias while a supported CLI release or documented
script still uses it. Any future retirement needs a CLI migration, usage
evidence and a separately announced compatibility decision.

The new direct tools are additive. A server should accept old and new MCP
clients during rollout. The advertised tool set, dynamic gateway catalog,
REST catalog, product allow-list and guidance must be updated together so a
tool is not visible in one path and missing in another.

## Build order

| # | Piece | Completion criterion |
|---|---|---|
| 1 | Contract and identity | Shared caller principal, connection ID, per-call visibility, Crew allow-list, workflow caller extension and normalized response types are specified and enforced. |
| 2 | Defaults and refusals | One effective-schema builder applies validated defaults, rejects unknown values and returns actionable problems without invented input values. |
| 3 | Core MCP tools | Four direct tools are advertised alongside the gateway; scope filtering, name resolution, durable polling and old aliases work end to end. |
| 4 | Workflow `ask` guidance | The assistant sees allowed functions and defaults, can choose a function or raw run, asks for missing important values and reports which path it used when known. |
| 5 | Function card | Run now, defaults, caller controls, progress/result and copy call use the same server contract. |
| 6 | Generated gateway targets | Dynamic, permission-filtered `get_api_spec` entries use collision-free IDs. |
| 7 | Function bindings | Webhook and schedule bindings, migration preview/action, delivery validation and history preservation. |
| 8 | Slack binding | Define channel routing and caller identity against the function binding contract. |

Steps 1–3 are the first MCP release. Steps 4–5 complete the direct-calling
experience. Steps 6–8 are independently shippable extensions.

## Verification

- MCP e2e: discovery returns both kinds with scoped pagination; exact IDs
  work; ambiguous names refuse; read-only tokens cannot call; a separate
  connection of the same user cannot read or continue a call.
- Call e2e: Crew and workflow `ask` continue the appropriate conversation;
  typed calls validate before dispatch; omitted defaults apply; explicit
  values win; null, unknown fields and invalid enums refuse; workflow group
  choice is discoverable and checked.
- Lifecycle: progress wakes `get_call`; idle timeout, late result, partial
  result and restart interruption map to the specified status and flags. A
  `"stop"` message does not falsely report cancellation.
- Permissions: base access and `allowed_callers` are checked at direct typed
  dispatch. `ask` lists only callable functions to the assistant, while raw
  Run-mode actions obey their own policy. Generated gateway and binding paths
  must use the same checks when those phases ship.
- Compatibility: old MCP names and the CLI scripts still work with existing
  responses and conversations; the new six-tool MCP listing is accurate.
- UI: a reader cannot run; an editor's call records their identity; defaults
  and last form values are distinguished; result and run link appear inline.
- Bindings: signature/auth and replay checks precede mapping; invalid
  mappings or values start no run; changed function schemas surface invalid
  bindings; a converted trigger preserves URL/secret or timing and history;
  unconverted triggers are untouched.
- Agentic check: a live MCP client given “ask the PR reviewer about PR 149”
  chooses `ask`, follows a long call, and reports the actual path taken. A
  separate client request naming `review_pr` uses `call_function` and supplies
  every missing required value before retrying.
