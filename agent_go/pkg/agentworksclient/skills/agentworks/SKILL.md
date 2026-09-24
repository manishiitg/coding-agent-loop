---
name: agentworks
description: Read and run AgentWorks workflows and Crews through the CLI or MCP bridge (list workflows, read files, plans, runs, guidance, and knowledge; execute steps, workflows, and schedules; ask Crews and call their functions). Load when the task touches an AgentWorks workflow or when agentworks tools are available.
---

# AgentWorks

This skill is an entry pointer, not a manual. All substantive guidance lives on the server and is fetched per task — nothing here can go stale.

This connection reads and runs, like the Slack and WhatsApp run-mode channels: tools read, and run-mode tools execute in pinned Run-mode sessions. Nothing creates, edits, or authors.

## Connect

```sh
agentworks login --server https://your-server
claude mcp add agentworks -e AGENTWORKS_SERVER='https://your-server' -- agentworks mcp serve
```

Approve the CLI in your browser. The CLI and MCP bridge share that connection. Its scopes allow reading (`workflows:read`, `files:read`) and running (`runs:execute`) workflows the account can access. Unavailable tools are omitted from the catalog.

## First step

Call `get_agent_context` for token capabilities, available tools, and the guidance version. Discover workflow IDs with `list_workflows` first — IDs are never filesystem paths.

## Guidance per task

List topics with `list_guidance_topics` and load only relevant ones via `get_guidance_topic`. Inspect workflow knowledge with `list_workflow_knowledge` / `read_workflow_knowledge` (learnings, knowledgebase notes, workspace skills, skill wiring). Use `get_file_link` for preview/download URLs and `files download` for local copies.

## Run

To run: call a run-mode tool such as `execute_step` — the reply carries `session_id` — then poll `run_status` for completion. Steer live work with `send_step_message`, stop it with `stop_step` / `stop_all_executions`, and read run evidence with `list_runs`, `get_run`, and `get_logs`. Schedules: `list_schedules`, `get_schedule_runs`, `trigger_schedule`.

## Chat

`chat` asks the workflow assistant anything — analysis, explanations, follow-ups — in a pinned Run-mode session. Pass `session_id` to continue the conversation; sessions are shared with the run tools, so one conversation can ask, run, and ask about the run. Read replies with `run_status`, and answer waiting human-input steps with `run_reply_input`.

## Crews

Crews are persistent AgentWorks agents. Discover them with `list_crews` (IDs, never paths); `get_crew` shows identity, model, and functions. Read project files with `list_crew_files` / `read_crew_file` (private chat transcripts and databases are never exposed). Call a Crew's typed functions with `call_crew_function` (arguments must match `list_crew_functions`), or ask anything with `ask_crew`. Both run as a turn in the Crew's own chat: the result returns within `wait_seconds` (max 25), otherwise poll `get_crew_function_call` with the returned `call_id` for progress and the result. Needs `crews:read` / `crews:run` on a token that includes the Crew.

## Answer from reading

If the task needs a change, say so instead of attempting one — authoring is not exposed.
