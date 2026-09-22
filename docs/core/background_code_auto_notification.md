# Trigger and auto notify

## Purpose

Give Builder chat and Crew chat a code-first asynchronous tool. The agent writes
small Python code that can wait for time or poll a condition. The tool returns
immediately. When the code exits, fails, or times out, the backend resumes the
**same chat** through the existing `[AUTO-NOTIFICATION]` path.

The initial implementation is available in ordinary Builder chat and writable
Crew chat. It is process-backed and does not survive a server restart. Durable
webhook suspension is future work.

## Agent-facing tool

Name: `trigger_and_auto_notify`.

```json
{
  "name": "Watch deployment abc123",
  "python": "import json, time, urllib.request\nwhile True:\n    with urllib.request.urlopen('https://api.example.test/deploy/abc123', timeout=10) as response:\n        deploy = json.load(response)\n    if deploy['status'] in ('healthy', 'failed'):\n        print(json.dumps(deploy))\n        break\n    time.sleep(30)",
  "timeout_seconds": 1200
}
```

The tool response is immediate:

```json
{
  "execution_id": "auto-notify-watc-0001",
  "status": "waiting",
  "name": "Watch deployment abc123"
}
```

The `python` field is ordinary Python. There is no `main(ctx)` contract or
notification API. The code prints the result it wants the resumed agent to see.
The backend records the originating session and conversation turn before
starting it, and the backend creates the auto notification when the process
finishes.

### Simple examples

```python
import time
time.sleep(500)
print("The waiting period is over; continue the task.")
```

```python
import time
while True:
    result = check_deployment_status("abc123")
    if result["state"] in ("healthy", "failed"):
        print(result)
        break
    time.sleep(30)
```

The second example is the intended polling pattern for this initial version.

## Auto notification contract

The backend formats and delivers the message; script output is treated as data.
One possible continuation is:

```text
[AUTO-NOTIFICATION] Agent 'Watch deployment abc123' completed — status=completed.
Result: Output: {"deployment_id":"abc123","status":"healthy"}
```

Use the existing background-agent registry, completion queue, busy-session live
steering, synthetic turns, and retry logic in `agent_go/cmd/server/background_agents.go`.
Keep the execution attached to its launching conversation turn. A busy chat can
receive a live steer when supported; otherwise the completion waits for the
session lane and is delivered by a synthetic turn. Delivery is committed only
after the receiving turn actually succeeds. A stopped session must not restart
because of an old job.

This auto notification goes to the **agent conversation**. It is distinct from
`notify_user`, which sends to human-facing channels such as Slack, Gmail,
WhatsApp, or the Org Dashboard. The resumed agent may choose `notify_user` when
the task and notification policy call for an external message.

## Execution lifecycle

1. Validate the code and timeout, resolve `python3`, and register the execution
   against the exact session and conversation turn that called the tool.
2. Return the execution ID without waiting for the code.
3. Run the plain code in an isolated Python interpreter process. Capture stdout
   and stderr up to 16 KiB each.
4. On process completion, error, or timeout, submit the result to the existing
   auto notification dispatcher.
5. Mark notification delivery only when the receiving agent turn succeeds;
   use the existing completion queue and retry behavior for a busy session.

## Webhook and restart model

An incoming webhook wait is not part of the initial implementation. Adding it
requires a platform-owned event broker. It must
use an authenticated connector or an explicitly created webhook endpoint,
persist registrations before accepting events, and deduplicate provider event
IDs.

The Python process cannot survive a server restart. Durable waits require a
separate persisted trigger model; replaying arbitrary Python is unsafe because
its external side effects may already have happened.

## Boundaries and defaults

- Available to Builder and writable Crew chats under the same user, workspace,
  and connector permissions as the originating turn. Read-only Crew sessions
  must not gain a background write path.
- Require a finite timeout between one second and 24 hours and limit captured
  stdout and stderr to 16 KiB each. CPU, memory, child-process, and network
  sandboxing remain follow-up hardening work.
- Do not allow script text or stdout to set a destination session, forge an
  `[AUTO-NOTIFICATION]` envelope, or call internal delivery APIs directly.
- Redact secrets from logs and notification previews. Give the worker only the
  credentials and event sources explicitly authorized for that job.
- Delivery uses the existing session completion queue.

## Follow-up sequence

1. Add process resource limits and a per-user active-trigger cap.
2. Add a persisted webhook trigger using one configured event source, exact
   event correlation, and duplicate-delivery tests.
3. Add durable trigger recovery without replaying arbitrary Python side effects.
4. Expand connector coverage and UI inspection after the lifecycle is reliable.

Current acceptance requires proving that the tool returns immediately; the notification
arrives in the launching chat even after switching tabs; a busy session queues
or accepts live steering; and completion, failure, and timeout wake the agent.

## Existing integration points

- Background execution registration and completion:
  `agent_go/cmd/server/delegation.go`
- Auto notification dispatch and synthetic turns:
  `agent_go/cmd/server/background_agents.go`
- Builder and Crew tool registration:
  `agent_go/cmd/server/server.go`, `agent_go/cmd/server/background_code_tools.go`
- Human-facing external delivery: `agent_go/cmd/server/virtual-tools/human_tools.go`
- Product webhook routes: `agent_go/cmd/server/product_webhooks.go` (confirm the
  exact router and permission model before reusing it)
