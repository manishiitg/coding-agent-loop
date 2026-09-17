---
name: work-schedules-and-bots
description: Manage Crew's message-only project schedules, authenticated webhook triggers, Slack or WhatsApp project-chat bots, and connected Gmail accounts. Use when the user asks for recurring work, event-driven work, a scheduled message, an API trigger, bot routing, email notifications, or reading Gmail in a Crew project.
---

# Crew schedules and bots

## Project schedules

- A Crew schedule contains exactly one message sent to this project's Builder
  conversation. It is not an AgentWorks workflow, route, phase, or execution.
- Use `list_project_schedules` before updating, deleting, or triggering. Use the
  exact returned schedule ID.
- For creation, provide a clear name, one complete message, a standard
  five-field cron expression, and an IANA timezone such as `Asia/Kolkata`.
  Enable it by default unless the user asks to save it paused.
- Use `trigger_project_schedule` only when the user asks to run it now. Report
  the resulting session without inventing a workflow run.

## Project webhook triggers

- A Crew webhook trigger contains one saved instruction. Each authenticated
  JSON delivery sends that instruction into this project's durable Builder
  conversation together with the workspace-relative payload file path.
- Use `list_project_triggers` before updating or deleting a trigger. Use the
  exact returned trigger ID.
- Use `create_project_trigger` with a clear name, one complete instruction,
  and either `bearer` or `github` authentication. Return the generated endpoint
  and one-time secret immediately; the secret cannot be listed later.
- Rotating a secret invalidates the old credential. Never write a trigger
  secret or raw delivery payload into `workflow.json`, chat instructions, logs,
  or source files.
- Crew triggers do not select or execute AgentWorks routes, steps, phases,
  Pulse, or workflow runs. Use an AgentWorks workflow webhook when those
  orchestration semantics are required.

## Project-chat bots

Slack and WhatsApp bot connections use the shared AgentWorks connector
infrastructure, but their route target is this Crew project chat. Configuration
is in **Setup > Bots**. If no bot-management tool is available in the current
turn, explain the exact UI location rather than pretending a route was created.
Bot messages inherit this project's workspace boundary, selected skills, MCP
servers, and available secrets.

## Gmail and Google Workspace

- Gmail is a shared account connection shown in **Setup > Bots**, not an MCP
  server and not an inbound project-chat bot. Check `list_gmail_connections`
  before searching the MCP catalog or claiming Gmail is unavailable.
- Before calling `google_workspace_cli`, use `list_skills` to check whether the
  upstream `gog` skills are installed. If they are missing, call
  `install_skill(source="https://github.com/openclaw/gogcli")`. Do this only
  when Google Workspace work needs the CLI guidance; opening a Crew project
  must not install external software or skills automatically.
- Use `read_skill` to load `gog` and the relevant installed `gog-gmail`,
  `gog-drive`, `gog-sheets`, `gog-docs`, `gog-slides`, or `gog-calendar`
  skill. Those versioned skills define current commands; do not infer syntax
  from old examples or retry by guessing.
- Pass the skill's command arguments to `google_workspace_cli` without the
  `gog` binary, account, client, home, or credential flags. The server selects
  the requested connection and injects its credential privately.
  Never request, print, or copy credential environment variables into chat or
  project files.
- Mailbox access requires both `allow_read_access` and an observed Google
  `gmail.readonly` grant. If requested and granted state differ, use
  `update_gmail_connection_grants` and tell the user to complete the returned
  reconnect flow. Do not install a Gmail MCP server as a workaround.
- Gmail sends are outbound notifications, not inbound bot routing. Slack and
  WhatsApp routes target this project; Gmail connection configuration remains
  account-wide and shared with AgentWorks.


## Slack bot routes and threaded messages

Call `get_slack_bot_settings` before changing a route and again afterward to confirm the saved state. Use exact Slack channel IDs (for example `C1234567890`), never display names. The tools are bound to the current workflow or Work project; they cannot manage another target.

`create_slack_bot_route(channel_id, bot_grant)` creates a route; `update_slack_bot_route_permission(channel_id, bot_grant)` changes its grant; `remove_slack_bot_route(channel_id)` revokes it. `test_slack_bot_connection` checks the connector. Global enablement and credentials remain operator configuration in Setup > Bots. Use `open_workspace_view(view="bots")` when the user needs that visual surface.

A grant authorizes the bot integration itself: `run` gives runtime/read-only authority; `owner` gives authoring authority. The Slack sender is audit metadata and never supplies execution permissions. Only an authenticated interactive owner may create, upgrade, downgrade, or revoke a grant. A Slack-origin session cannot manage grants even if its bot has an `owner` grant. Read-only sessions receive inspection tools, not mutation tools. Permission changes invalidate running bot work and are rechecked on every turn.

Workflow destinations contain `workflow_id` and `workspace_path`. Product destinations contain `profile_id`, `conversation_key`, and `workspace_path`. A Work project route must preserve `profile_id=work` and `conversation_key=<projectId>` and must not acquire a workflow ID. The backend binds the product owner; never edit `workspace_user_id`.

## Scripted and agentic threaded sends

Use `send_slack_message(route_id, message, idempotency_key, thread_ref?)` through the existing authenticated MCP bridge. `route_id` is the exact configured channel route ID. The message limit is 3000 characters per logical send. Both grants may send; sending never confers permission to manage grants.

Outside a Slack-origin invocation, omitting `thread_ref` starts a top-level message. The response includes `channel_id`, `message_ts`, and an opaque `thread_ref`. Supply that reference for replies. Slack-origin runs and their children automatically inherit the originating thread when the reference is omitted.

For cross-step delivery, write the returned `thread_ref` to the scripted step's declared context output. A downstream step declares that file in `context_dependencies`, reads its normal positional input, and passes the reference to `send_slack_message`. Use the existing `MCP_AUTH` bridge; never place bot/app tokens in `SECRET_*`, scripts, prompts, outputs, or environment variables.

Reuse the same idempotency key for retries of one logical send. Use a different key for a different message. If the service reports an uncertain delivery outcome, inspect Slack before sending again: a new key could duplicate a message already accepted by Slack. Arbitrary channels and cross-route references are rejected, and a revoked route cannot send.

## Deterministic listeners

A saved route may include a `trigger`: `human_message` or `trusted_app`, an optional text `contains` filter, and exact trusted `app_id`/`bot_id` values for app messages. Only top-level messages trigger automation; edits, thread replies, and this bot's own messages do not. A workflow trigger fixes `group_names` plus either `route_selections` or `step_id` in owner configuration. The backend validates these against the saved plan on save and dispatch. Event content cannot change the target, grant, groups, or selected routes.

Events are stored as untrusted data. Preserve normal approvals for high-risk actions. Send progress and the final result/RCA with `send_slack_message` into the inherited source thread. Slack workflow runs use server-bound immutable `iteration-N-slack-<id>` folders and must never use or rotate Builder's `iteration-0`.

One global Slack connector may serve multiple channels. Each channel has one workflow or project destination. Everyone in that channel can invoke its bot grant without an AgentWorks login. Owners may set `blocked_emails` on create/update route tools; `[]` clears exclusions. Exclusions apply to human messages and approvals. With exclusions configured, an unverifiable email is blocked; trusted configured app triggers retain their source-identity checks. Sends are one-shot; the backend never automatically retries an uncertain Slack post.
