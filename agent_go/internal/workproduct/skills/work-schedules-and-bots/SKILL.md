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
