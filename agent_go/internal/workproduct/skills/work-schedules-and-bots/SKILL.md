---
name: work-schedules-and-bots
description: Manage Work's message-only project schedules, authenticated webhook triggers, and Slack or WhatsApp project-chat bots. Use when the user asks for recurring work, event-driven work, a scheduled message, an API trigger, an immediate schedule run, or bot routing into a Work project.
---

# Work schedules and bots

## Project schedules

- A Work schedule contains exactly one message sent to this project's Builder
  conversation. It is not an AgentWorks workflow, route, phase, or execution.
- Use `list_project_schedules` before updating, deleting, or triggering. Use the
  exact returned schedule ID.
- For creation, provide a clear name, one complete message, a standard
  five-field cron expression, and an IANA timezone such as `Asia/Kolkata`.
  Enable it by default unless the user asks to save it paused.
- Use `trigger_project_schedule` only when the user asks to run it now. Report
  the resulting session without inventing a workflow run.

## Project webhook triggers

- A Work webhook trigger contains one saved instruction. Each authenticated
  JSON delivery sends that instruction into this project's durable Builder
  conversation together with the workspace-relative payload file path.
- Use `list_project_triggers` before updating or deleting a trigger. Use the
  exact returned trigger ID.
- Use `create_project_trigger` with a clear name, one complete instruction,
  and either `bearer` or `github` authentication. Return the generated endpoint
  and one-time secret immediately; the secret cannot be listed later.
- Rotating a secret invalidates the old credential. Never write a trigger
  secret or raw delivery payload into `product.json`, chat instructions, logs,
  or source files.
- Work triggers do not select or execute AgentWorks routes, steps, phases,
  Pulse, or workflow runs. Use an AgentWorks workflow webhook when those
  orchestration semantics are required.

## Project-chat bots

Slack and WhatsApp bot connections use the shared AgentWorks connector
infrastructure, but their route target is this Work project chat. Configuration
is in **Setup > Bots**. If no bot-management tool is available in the current
turn, explain the exact UI location rather than pretending a route was created.
Bot messages inherit this project's workspace boundary, selected skills, MCP
servers, and available secrets.
