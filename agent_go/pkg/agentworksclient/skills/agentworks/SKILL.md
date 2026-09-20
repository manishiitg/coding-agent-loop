---
name: agentworks
description: Read and run AgentWorks workflows through the CLI or MCP bridge (list workflows, read files, plans, runs, guidance, and knowledge; execute steps, workflows, and schedules). Load when the task touches an AgentWorks workflow or when agentworks tools are available.
---

# AgentWorks

This skill is an entry pointer, not a manual. All substantive guidance lives on the server and is fetched per task — nothing here can go stale.

This connection reads and runs, like the Slack and WhatsApp run-mode channels: tools read, and run-mode tools execute in pinned Run-mode sessions. Nothing creates, edits, or authors.

## Connect

```sh
printf '%s' '<token>' | agentworks login --server https://your-server --token-stdin
claude mcp add --transport stdio --env AGENTWORKS_TOKEN='<token>' agentworks -- agentworks mcp serve
```

Tokens read (`workflows:read`, `files:read`) and, when granted, run (`runs:execute`); all are limited to workflows the account can access. Unavailable tools are omitted from the catalog.

## First step

Call `get_agent_context` for token capabilities, available tools, and the guidance version. Discover workflow IDs with `list_workflows` first — IDs are never filesystem paths.

## Guidance per task

List topics with `list_guidance_topics` and load only relevant ones via `get_guidance_topic`. Inspect workflow knowledge with `list_workflow_knowledge` / `read_workflow_knowledge` (learnings, knowledgebase notes, workspace skills, skill wiring). Use `get_file_link` for preview/download URLs and `files download` for local copies.

## Run

To run: call a run-mode tool such as `execute_step` — the reply carries `session_id` — then poll `run_status` for completion. Steer live work with `send_step_message`, stop it with `stop_step` / `stop_all_executions`, and read run evidence with `list_runs`, `get_run`, and `get_logs`. Schedules: `list_schedules`, `get_schedule_runs`, `trigger_schedule`.

## Chat

`chat` asks the workflow assistant anything — analysis, explanations, follow-ups — in a pinned Run-mode session. Pass `session_id` to continue the conversation; sessions are shared with the run tools, so one conversation can ask, run, and ask about the run. Read replies with `run_status`, and answer waiting human-input steps with `run_reply_input`.

## Answer from reading

If the task needs a change, say so instead of attempting one — authoring is not exposed.
