---
name: agentworks
description: Read AgentWorks workflows through the CLI or MCP bridge (list workflows, read files, plans, runs, guidance, and knowledge). Load when the task touches an AgentWorks workflow or when agentworks tools are available.
---

# AgentWorks

This skill is an entry pointer, not a manual. All substantive guidance lives on the server and is fetched per task — nothing here can go stale.

This connection is read-only, like the Slack and WhatsApp run-mode channels: every tool reads; nothing creates, edits, or runs.

## Connect

```sh
printf '%s' '<token>' | agentworks login --server https://your-server --token-stdin
claude mcp add --transport stdio --env AGENTWORKS_TOKEN='<token>' agentworks -- agentworks mcp serve
```

Tokens are read-only (`workflows:read`, `files:read`) and limited to workflows the account can access. Unavailable tools are omitted from the catalog.

## First step

Call `get_agent_context` for token capabilities, available tools, and the guidance version. Discover workflow IDs with `list_workflows` first — IDs are never filesystem paths.

## Guidance per task

List topics with `list_guidance_topics` and load only relevant ones via `get_guidance_topic`. Inspect workflow knowledge with `list_workflow_knowledge` / `read_workflow_knowledge` (learnings, knowledgebase notes, workspace skills, skill wiring). Use `get_file_link` for preview/download URLs and `files download` for local copies.

## Answer from reading

If the task needs a change, say so instead of attempting one — mutations are not exposed in v1.
