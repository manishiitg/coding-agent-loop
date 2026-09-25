# Calling Crews and workflows: one model, one set of tools

Status: design, 2026-09-25. Not built. Builds on
[crew-calls.md](../crew-calls.md), which covers functions, `ask`, callers and
timeouts as they exist today.

## Why

A person, a Crew, an agent over MCP, or Slack all want the same two things
from a Crew or a workflow: **ask it something**, or **run one of its
functions with some inputs**. Today each path looks different:

- **Over MCP, Crews and workflows use different tool sets.** Crews have
  `get_crew` and `ask_crew`. Workflows have `get_workflow`, no
  `ask_workflow`, and two ways to ask: `call_workflow_function` with
  `function: "ask"`, and the older `chat` plus `run_status`. The connection's
  instructions describe workflow functions as typed runs only ("never a
  free-text task"), so agents don't offer to ask a workflow at all.
  (Observed 2026-09-25: an MCP client offered `get_crew` / `ask_crew` for
  Crews, but only "plan, files, runs, knowledge" for workflows.)
- **Functions, webhooks and schedules are three separate things.** Each has
  its own screen, its own settings and its own way to call it, although each
  one really means "run this route with these inputs".
- **Running a function takes typing every input.** The owner and repo are
  repeated on every `review_pr` call, and the UI has no Run button.
- **A refused call returns only an error**, not the function's inputs or an
  example of a correct call.

## Principles

1. **A function is the unit.** A function is a route or task plus typed
   inputs. Everything that starts it is a *caller*: a person in the UI, a
   Crew, an MCP or CLI connection, Slack, a webhook, a schedule.
2. **Crews and workflows have one surface.** The same verbs apply to both;
   the kind is a detail of the target.
3. **`ask` is the front door; functions are the fast lane.** Anyone can ask
   in plain words and the assistant picks the function and inputs. Callers
   that know the function call it directly.
4. **The server already does most of this.** Crew and workflow calls share
   `startCrewFunctionCall`, the per-caller conversations, activity-based
   timeouts and late answers. The work is mostly surface: tool definitions,
   the UI, instruction text.

---

## Part 1: MCP and CLI

### One set of tools

The server knows a target's kind from its ID (workflow IDs start with
`wf_`; Crew IDs are UUIDs) or from its name.

| Tool | Does | Replaces |
|---|---|---|
| `list_agents` | Crews and workflows together. Per item: `id`, `kind`, `name`, a one-line purpose, and its functions (name plus required inputs). | `list_crews`, `list_workflows` |
| `get_agent` | One Crew or workflow: identity, model or plan summary, full function schemas, recent activity. | `get_crew`, `get_workflow` |
| `ask` | A free-text question. One continuing conversation per caller per target. Returns the answer, or a `call_id` if it takes longer. | `ask_crew`, `call_workflow_function(function: "ask")`, most uses of `chat` |
| `call_function` | Runs a typed function. Inputs are checked first. Returns the result, or a `call_id`. | `call_crew_function`, `call_workflow_function` |
| `get_call` | Status, latest progress, and the result of an `ask` or `call_function`. | `get_crew_function_call`, `get_workflow_function_call` |

These stay, for work that is really about workflows: `list_runs`, `get_run`,
`run_status`, `run_reply_input`, `get_plan`, file and knowledge reads, and
`chat` (see below).

**`target` accepts an ID or a name.** `ask("RTS Flow Tester", …)` and
`call_function("rts-pr-reviweer", "review_pr", …)` resolve by name,
ignoring case and punctuation. If a name matches several targets, the call
is refused with the list of matches and their IDs. The resolved ID is
included in every response, so later calls can use it.

**`ask` details:**
- `new_conversation: true` starts a fresh thread instead of continuing the
  caller's existing one.
- A workflow's `ask` goes to its Run-mode assistant, as today. The assistant
  can answer questions, start runs with the right variables and report the
  outcome, and file suggestions for the owner.
- `chat` stays for the advanced case: several separate sessions on one
  workflow, or following a specific run with `run_status`. The instructions
  mention it only after `ask`.

**Waiting and polling.** An MCP call can't block for long, so `ask` and
`call_function` wait up to `wait_seconds` (max 25) and then return
`{status: "running", call_id, progress}`. Every response includes the
target's latest progress line (for example "turn 3 of 5, leak scan clean so
far"), so the agent can relay it instead of going quiet. The instructions say
plainly: *if you get a `call_id`, call `get_call` until status is completed
or failed*. A late answer after a timeout is delivered the same way as
today.

**Helpful refusals.** A refused `call_function` returns:
- the exact problem ("missing required input `PR_NUMBER`");
- the function's input schema, with defaults filled in (Part 2);
- a ready-to-copy corrected call.

An unknown function name returns the target's function list.

### Compatibility

The old tool names keep working as aliases: same handlers, same responses.
They are dropped from the advertised list and the instructions, so new agents
learn only the new names. Old names are removed after one release with no
recorded use; the API already logs calls per tool.

### Connection instructions (replaces the Crew and workflow paragraphs)

> AgentWorks has Crews (persistent agents) and workflows (automations).
> Treat both the same way. `list_agents` shows them with their functions.
> To ask anything, use `ask(target, message)`; each target keeps one
> continuing conversation with you. To run something specific, use
> `call_function(target, function, args)`; inputs are checked before
> anything runs. Targets can be names or IDs. If a response has a `call_id`,
> call `get_call` until it finishes and pass on its progress to the user.

### CLI

Top-level commands, the same verbs:

```
agentworks agents                                  # list_agents
agentworks agent "RTS Flow Tester"                 # get_agent
agentworks ask "RTS Flow Tester" "re-run the Hebrew leak check"
agentworks ask rts-pr-reviweer "why was PR 149 skipped?" --new
agentworks call rts-pr-reviweer review_pr --PR_NUMBER 149
agentworks call-status fn-…
```

`call` maps `--NAME value` flags to inputs. Typed values follow the
function's schema. Any missing required input produces the same helpful
refusal as MCP. The existing `crews` and `functions` command groups stay as
aliases.

### Generated tools per function (in-platform chats)

Crew chats already get one tool per function of each attached Crew or
workflow (for example `rts_pr_reviewer__review_pr`). They keep them. Over
MCP, the same generated names are listed by `get_api_spec` under a
`functions` section. `call_tool("rts_pr_reviewer__review_pr", {...})` then
works directly, with no list-then-call step. MCP still exposes only its two
fixed tools; the generated names are call targets, not new MCP tools.

---

## Part 2: running functions

### The function card

Automation → Functions shows one card per function, for Crews and
workflows. The built-in `ask` is always listed first.

| Section | What it does |
|---|---|
| **Run now** | A form built from the input schema: text fields, number fields, dropdowns for `enum`, checkboxes for booleans, defaults filled in. **Run** starts the call with the viewer as the caller. The result, progress and a link to the run show inline. |
| **Defaults** | A saved default value per input (Part 2.2). |
| **Schedule** | Run on a schedule with fixed inputs. Several schedules per function. |
| **Webhook** | A URL for outside systems, with a mapping from payload fields to inputs (e.g. `pull_request.number` → `PR_NUMBER`) and the existing auth modes. |
| **Slack** | Channels or apps that may start it, e.g. a message such as "review PR 149" in a routed channel. |
| **Callers** | Crews, people and connections allowed to call it (`allowed_callers`), plus each caller's continuing conversation. |
| **Copy call** | The same call as an MCP `call_function`, a CLI command, a curl command against the external API, or a Slack message. |

**Who can use Run now:** people with run access to the workflow, and the
owner and editors of a Crew. Read-only viewers see the card but not the Run
button.

### Default inputs

`WorkflowFunctionInput` and a Crew function's `input_schema` properties get an
optional `default`:

```json
{"name": "GITHUB_OWNER", "type": "string", "required": true, "default": "runloop-works"}
```

- A required input with a default can be left out by any caller. The
  default is applied before validation, so every existing check still holds.
- The Run now form, the refusal message and `get_agent` all show defaults.
- **Per-caller last values:** the UI pre-fills the viewer's last inputs for
  that function, kept in browser storage only. The server does not remember
  per-caller values, so a call never depends on hidden state.

With defaults for owner and repo, `review_pr` needs only `PR_NUMBER`.

### Webhooks and schedules become callers

**Data model:** a function trigger (`kind: "function"`) is the function. A
webhook or schedule that targets it stores:
- `function` (the trigger ID);
- `inputs` (fixed values, for schedules);
- `input_mapping` (payload path to input, for webhooks).

It no longer stores its own route selection and allowed variables. A delivery
goes through the same `dispatchWorkflowFunction` path, so inputs are checked
the same way for every caller.

**Migration:**
- An existing webhook or schedule with a route selection and allowed
  variables keeps working as a *standalone* trigger. The Webhooks view shows
  it as today, with a "Turn into a function" action. That action creates a
  function from its route and variables and re-points the trigger at it.
- Nothing is converted automatically.

**Crews:** schedules on a Crew already deliver a prompt. A Crew function
schedule delivers `call_function` with fixed inputs, into the schedule's own
caller conversation.

### `ask` picks the function

A workflow's assistant already starts runs from free text. Two changes make
that the norm:
1. The assistant's `ask` prompt lists the workflow's functions with inputs
   and defaults. It prefers starting a function over a raw run, so the
   inputs are checked the same way as a direct call.
2. When a required input is missing, the assistant asks for exactly that
   input in one question, instead of guessing or failing.

A Crew's `ask` already sees its functions in its own chat; no change.

---

## Build order

| # | Piece | Size | Notes |
|---|---|---|---|
| 1 | `default` on inputs, applied before validation | S | Server only. Unblocks the short calls. |
| 2 | Helpful refusals: schema, defaults, corrected call | S | Server only; all callers benefit. |
| 3 | MCP and CLI: `list_agents`, `get_agent`, `ask`, `call_function`, `get_call`, name resolution, aliases, new instructions | M | Mostly wrappers over existing handlers. |
| 4 | Function card: Run now form, defaults, copy call | M | Frontend plus one "run as viewer" endpoint that reuses `startCrewFunctionCall`. |
| 5 | Workflow `ask` prompt lists functions and asks for missing inputs | S | Prompt text plus a test. |
| 6 | Generated per-function names in `get_api_spec` | S | |
| 7 | Webhooks and schedules as function callers, with "Turn into a function" | L | Data model change and migration UI. Do last. |

Steps 1–3 give MCP and CLI users the consistent experience. Step 4 is the
biggest gain for people in the UI.

## Tests

- **MCP (e2e against a local server):**
  - `list_agents` returns both kinds.
  - `ask` by name reaches a Crew and a workflow, and continues the same
    conversation.
  - `call_function` with a missing input returns the schema and a corrected
    call.
  - `get_call` returns progress, then the result.
  - The old names still work.
- **Defaults:** a required input with a default can be omitted; an explicit
  value wins; an invalid default is refused when the function is saved.
- **Run now:** a reader sees no Run button; an editor's run is stamped with
  their caller identity, and the result appears inline.
- **Webhook-to-function migration:** a converted trigger delivers the same
  run as before; an unconverted one is untouched.
- **Agentic check (per the E2E rule for LLM code):** a live MCP client given
  only "ask the PR reviewer about PR 149" picks `ask` with the name and
  relays progress. It signs off in JSON.

## Open questions

1. **Name resolution scope.** Should `ask("latency")` match a Crew and a
   workflow both named "latency", or prefer one kind? Proposed: refuse with
   both matches. Being explicit is cheap.
2. **Run now for Crews.** Crew functions run in the viewer's own caller
   conversation with the Crew. Proposed: yes, the same as MCP. The run shows
   up in the Crew's Chats as "Called by <viewer>".
3. **Should `chat` be removed eventually?** Proposed: keep it; it's the only
   way to run several parallel sessions on one workflow.
