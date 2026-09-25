---
name: agentworks
description: Discover, ask, and call AgentWorks Crews and workflows through MCP, and read their files, plans, runs, and guidance. Load when the task touches an AgentWorks Crew or workflow or when AgentWorks MCP tools are available.
---

# AgentWorks

This skill is an entry pointer. Discover current tool schemas and detailed guidance from the server when the task needs them.

This connection reads and runs, like the Slack and WhatsApp run-mode channels: tools read, and run-mode tools execute in pinned Run-mode sessions. Nothing creates, edits, or authors.

## Connect

```sh
claude mcp add --transport http agentworks 'https://your-server/api/external/v1/mcp'
```

Approve the MCP connection in your browser. Its scopes control reading (`workflows:read`, `files:read`, `crews:read`) and calling (`runs:execute`, `crews:run`) targets the account can access. The remote MCP surface advertises `list_agents`, `ask`, `call_function`, and `get_call` directly when permitted. Use `get_api_spec` and `call_tool` for other operations. Unavailable tools are omitted.

## First step

Use `list_agents` to find visible Crews and workflows by ID or name. Use a returned ID for calls; IDs are never filesystem paths. `get_api_spec` lists other permitted operations and their schemas; call `get_agent_context` through `call_tool` if you need your capabilities and guidance version.

## Ask and call

Use `ask(target, message)` for a plain-language request. The target gets one continuing conversation for this MCP connection. A workflow assistant may choose an exposed typed function or a raw Run-mode action; use `call_function(target, function, args)` when a specific function and its inputs are known and validation before execution matters. Omitted declared defaults are applied, explicit values override them, and invalid or unknown inputs are refused. A refusal includes problems and the effective schema; supply missing values before retrying. If a call is still working, follow its `call_id` with `get_call`. A separate access token owned by the same user has a separate conversation and cannot poll that call.

## Guidance per task

List topics with `list_guidance_topics` and load only relevant ones via `get_guidance_topic`. Inspect workflow knowledge with `list_workflow_knowledge` / `read_workflow_knowledge` (learnings, knowledgebase notes, workspace skills, skill wiring). Use `get_file_link` for preview/download URLs.

## Run

To run: call a run-mode tool such as `execute_step` — the reply carries `session_id` — then poll `run_status` for completion. Steer live work with `send_step_message`, stop it with `stop_step` / `stop_all_executions`, and read run evidence with `list_runs`, `get_run`, and `get_logs`. Schedules: `list_schedules`, `get_schedule_runs`, `trigger_schedule`.

## Chat

`chat` asks the workflow assistant anything — analysis, explanations, follow-ups — in a pinned Run-mode session. Pass `session_id` to continue the conversation; sessions are shared with the run tools, so one conversation can ask, run, and ask about the run. Read replies with `run_status`, and answer waiting human-input steps with `run_reply_input`.

## Crews

The older `list_workflow_functions`, `call_workflow_function`, and `get_workflow_function_call` operations remain available through `call_tool` for existing scripts. For new MCP calls, use the direct tools above. Workflow typed functions return run outcomes. Workflow `ask` reaches its Run-mode assistant; it can choose a typed function or a raw run. Calls need `runs:execute` and edit access to the workflow.

Crews are persistent AgentWorks agents. `get_crew` shows identity, model, and functions; `list_crew_files` / `read_crew_file` read shared project files. The older `list_crews`, `call_crew_function`, `ask_crew`, and `get_crew_function_call` operations remain available through `call_tool` for existing scripts. They keep their legacy user-scoped conversation. New direct calls use the connection-scoped conversation. Calls need `crews:run` on a token that includes the Crew.

## Answer from reading

If the task needs a change, say so instead of attempting one — authoring is not exposed.
