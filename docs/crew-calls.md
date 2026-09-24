# Calling a Crew: functions, callers and webhooks

There is one way to call a Crew, and one kind of automation a Crew runs on its
own.

| You want to… | Use | Runs in |
|---|---|---|
| Have another Crew, a workflow, or an MCP/CLI tool use a Crew | **Functions** (`ask` or a typed function) | The caller's own continuing conversation with that Crew |
| Run something on a timer | **Schedule** | The Crew's main chat, or the schedule's own conversation |
| Let an outside system (GitHub, CI) start work | **Webhook** | The Crew's main chat, or the webhook's own conversation |

The Crew's **main chat is for people**. Calls from other Crews, workflows and
tools never land there.

## Functions

Every Crew has the built-in **`ask`**: free text in, and its final reply
comes back as the answer. A Crew can also offer **typed functions**, such
as `run_login_flow(build, env) → {passed, failed_step}`. For these, the
arguments are checked before the call and the result is checked before it is
returned.

- **From a Crew or Builder chat:** use `list_functions(target)`, then
  `call_function(target, function, args)`. Tagging `#crew:<name>` in a message
  also generates a `<crew>__<function>` tool.
- **From MCP or the CLI:** use `ask_crew` / `call_crew_function`, or
  `agentworks crews ask` / `agentworks crews call`.
- **Getting the result:** a call that finishes within about 2 minutes (25
  seconds over MCP/CLI) returns its result directly. A longer call returns a
  `call_id`; a calling Crew gets the result as an automatic notification, and
  MCP/CLI clients poll `get_crew_function_call`.
- **While a call runs:** `get_function_call` shows progress without
  interrupting. `ask_function_update` sends a question or extra details into
  the running call.

## Workflow functions

A workflow is not a conversational agent, so it has no `ask`. It offers the
**functions its Builder exposes**. Each one is a trigger of kind `function`
with:

- a fixed route and allowed groups, like a webhook;
- typed inputs, such as `review_pr(GITHUB_OWNER, GITHUB_REPO, PR_NUMBER)`.
  Each input is a declared workflow variable and is set for that run.

A call with a missing, unknown or mistyped input is **refused before anything
runs**, for example `review_pr: missing required input PR_NUMBER`. It never
falls back to a saved value. The caller gets the run's outcome: its status,
any error, and each step's output (a skipped step says why).

To add one, ask the workflow's Builder, for example "expose the review route
as review_pr taking GITHUB_OWNER, GITHUB_REPO and PR_NUMBER (all required)".
The workflow's **Webhooks** tab lists its functions under **Functions**.

**Who may call:** a function has no URL and no secret. The platform identifies
the caller, and nothing in the request can change that:

- a Crew or workflow chat, running for a user with edit access to the
  workflow;
- an MCP/CLI access token with `runs:execute` that includes the workflow.

`function.allowed_callers` can narrow this to named Crews or workflows. Every
run records who called it.

**Webhooks stay strict:** a GitHub webhook on the same route keeps its
signature check, raw payload and key validation. The function checks only its
declared inputs. Both feed the same workflow variables.

- **From MCP/CLI:** `list_workflow_functions`, `call_workflow_function`,
  `get_workflow_function_call`, or `agentworks functions list|call|call-status
  --workflow <id>`.

## One continuing conversation per caller

Each caller gets **one continuing conversation** with each Crew it calls. The
caller is another Crew, a workflow, or an AgentWorks user connecting through
MCP or the CLI. Follow-up calls land in the same conversation, so the Crew
remembers earlier calls from that caller. Calling a Crew with `ask_crew`
repeatedly is effectively a chat with it. Different callers never share a
conversation, and none of them sees the main chat.

The conversation is set up automatically on the first call. The Crew's
**Automation → Functions** tab lists these under **Callers**; **Disconnect**
removes one, and the caller's next call starts a new conversation. Each
caller's conversation appears in the Crew's **Chats** list.

## Webhooks and schedules

The **Webhooks** tab lists external webhooks only: URL, auth mode (Bearer or
GitHub signature), and where each one runs. **Main chat** puts the run into
the Crew's main conversation. **Own conversation** gives the webhook a
continuing conversation of its own. Schedules offer the same choice.
