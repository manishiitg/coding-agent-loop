---
name: agentworks
description: Work with AgentWorks workflows through the CLI or MCP bridge (list workflows, read/edit files and plans, share assets, Builder chat). Load when the task touches an AgentWorks workflow or when agentworks tools are available.
---

# AgentWorks

This skill is an entry pointer, not a manual. All substantive guidance lives on the server and is fetched per task — nothing here can go stale.

## Connect

```sh
agentworks login --server https://your-server --token-stdin
claude mcp add --transport stdio agentworks -- agentworks mcp serve
```

Tokens are scoped (`workflows:read`, `files:read`, `files:write`, `plan:write`, `builder:chat`). Unavailable tools are omitted from the catalog; a restricted token cannot gain access by other means.

## Mandatory first step

Call `get_agent_context` before plan changes (pass `action: plan_change` for edits, `file_edit`, `share_asset`, or `builder_chat` otherwise). It returns token capabilities, available tools, the guidance version, and the preparation checklist. Discover workflow IDs with `list_workflows` first — IDs are never filesystem paths.

## Guidance per task

List topics with `list_guidance_topics` and load only relevant ones via `get_guidance_topic`. Always load `plan-change-impact` before treating a plan change as done. Inspect workflow knowledge with `list_workflow_knowledge` / `read_workflow_knowledge` (learnings, knowledgebase notes, workspace skills, skill wiring).

## Edit discipline

Pass `expected_revision` from `get_plan` / `read_file`; re-read on `revision_conflict`, never blind-retry. Every mutation needs a `reason`. Every plan mutation returns `required_followups` — complete them before the change is done. `workflow_busy` means a run or builder turn is active: wait or coordinate via the builder session.

## When not to use direct tools

For planning work that depends on AgentWorks conventions, prefer `builder_chat` (start without `session_id`, poll `builder_status`, answer blocked inputs with `builder_reply_input`). Direct plan tools are structurally safe but carry no decision process.
