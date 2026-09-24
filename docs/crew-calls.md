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

Every Crew and workflow has the built-in **`ask`**: free text in, and its final
reply comes back as the answer. A Crew can also offer **typed functions**, such
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
